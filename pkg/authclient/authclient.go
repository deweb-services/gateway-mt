// Copyright (C) 2021 Storj Labs, Inc.
// See LICENSE for copying information.

package authclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path"

	"go.uber.org/zap"

	"github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"

	"storj.io/common/lrucache"
	"storj.io/gateway-mt/pkg/auth/authdb"
	"storj.io/gateway-mt/pkg/errdata"
	"storj.io/gateway-mt/pkg/middleware"
)

var mon = monkit.Package()

// AuthClient communicates with the Auth Service.
type AuthClient struct {
	Config
	// Cache is used for caching authservice's responses.
	Cache *lrucache.ExpiringLRU
}

// New returns a new auth client.
func New(config Config) *AuthClient {
	return &AuthClient{
		Config: config,
		Cache: lrucache.New(lrucache.Options{
			Expiration: config.Cache.Expiration,
			Capacity:   config.Cache.Capacity,
		}),
	}
}

// Resolve maps an access key into an auth service response. clientIP is the IP
// of the client that originated the request and it's required to be sent to the
// Auth Service.
func (a *AuthClient) Resolve(ctx context.Context, accessKeyID string, clientIP string) (_ AuthServiceResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if len(accessKeyID) == 0 {
		return AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.New("Access Key ID is empty"), http.StatusBadRequest)
	}
	if err = (&authdb.EncryptionKey{}).FromBase32(accessKeyID); err != nil {
		return AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.New("Access Key ID is invalid: %w", err), http.StatusBadRequest)
	}

	reqURL, err := url.Parse(a.BaseURL)
	if err != nil {
		return AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusInternalServerError)
	}

	reqURL.Path = path.Join(reqURL.Path, "/v1/access", accessKeyID)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL.String(), nil)
	if err != nil {
		return AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusInternalServerError)
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Forwarded", "for="+clientIP)
	middleware.AddRequestIDToHeaders(req)

	delay := a.BackOff
	client := http.Client{
		Timeout:   a.Timeout,
		Transport: &http.Transport{ResponseHeaderTimeout: a.Timeout},
	}
	for {
		resp, err := client.Do(req)
		if err != nil {
			if !delay.Maxed() {
				if err := delay.Wait(ctx); err != nil {
					return AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), errdata.HTTPStatusClientClosedRequest)
				}
				continue
			}
			return AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusInternalServerError)
		}

		// Use an anonymous function for deferring the response close before the
		// next retry and not pilling it up when the method returns.
		retry, authResp, err := func() (retry bool, _ AuthServiceResponse, _ error) {
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusInternalServerError {
				return true, AuthServiceResponse{}, nil // auth only returns this for unexpected issues
			}

			if resp.StatusCode != http.StatusOK {
				return false, AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.New("%s", resp.Status), resp.StatusCode)
			}

			var authResp AuthServiceResponse
			if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
				if !delay.Maxed() {
					return true, AuthServiceResponse{}, nil
				}
				return false, AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusInternalServerError)
			}

			return false, authResp, nil
		}()

		if retry {
			if err := delay.Wait(ctx); err != nil {
				return AuthServiceResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), errdata.HTTPStatusClientClosedRequest)
			}
			continue
		}

		return authResp, err
	}
}

// ResolveWithCache is like Resolve, but it uses the underlying LRU cache to
// cache and returns cached authservice's successful responses if caching is
// enabled.
func (a *AuthClient) ResolveWithCache(ctx context.Context, accessKeyID string, clientIP string) (_ AuthServiceResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if a.Cache == nil {
		return a.Resolve(ctx, accessKeyID, clientIP)
	}

	v, err := a.Cache.Get(ctx, accessKeyID, func() (interface{}, error) {
		response, err := a.Resolve(ctx, accessKeyID, clientIP)

		switch errdata.GetStatus(err, http.StatusOK) {
		case http.StatusOK, http.StatusNotFound:
			encResp, encErr := encryptResponse(accessKeyID, response, err)
			if encErr != nil {
				return cachedAuthServiceResponse{err: err}, encErr
			}
			return encResp, nil
		default:
			return cachedAuthServiceResponse{}, err // err is already wrapped
		}
	})
	if err != nil {
		return AuthServiceResponse{}, err // err is already wrapped
	}

	response := v.(cachedAuthServiceResponse)

	decResp, err := response.decrypt(accessKeyID)
	if err != nil {
		return AuthServiceResponse{}, err
	}

	return decResp, response.err
}

// GetHealthLive returns the auth service health live status.
func (a *AuthClient) GetHealthLive(ctx context.Context) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)

	baseURL, err := url.Parse(a.BaseURL)
	if err != nil {
		return false, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusBadRequest)
	}
	healthLiveURL, err := baseURL.Parse("/v1/health/live")
	if err != nil {
		return false, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusBadRequest)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", healthLiveURL.String(), nil)
	if err != nil {
		return false, AuthServiceError.Wrap(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	client := http.Client{
		Timeout:   a.Timeout,
		Transport: &http.Transport{ResponseHeaderTimeout: a.Timeout},
	}
	res, err := client.Do(req)
	if err != nil {
		return false, AuthServiceError.Wrap(err)
	}
	defer func() { err = errs.Combine(err, AuthServiceError.Wrap(res.Body.Close())) }()
	if res.StatusCode != http.StatusOK {
		return false, AuthServiceError.New("%s", res.Status)
	}
	return true, nil
}

// CheckBucketIsUnique checks if bucket name is unique in auth service
func (a *AuthClient) CheckBucketIsUnique(ctx context.Context, bucketName string, clientIP string, logger *zap.SugaredLogger) (_ BucketIsUniqueResponse, err error) {
	defer logger.Info("start auth client check bucket")
	logger.With("bucket name", bucketName, "base url", a.BaseURL, "token", a.Token).Debug("auth client check bucket")
	defer logger.Info("end auth client check bucket")
	defer mon.Task()(&ctx)(&err)

	if len(bucketName) == 0 {
		return BucketIsUniqueResponse{}, errdata.WithStatus(AuthServiceError.New("Bucket name is empty"), http.StatusBadRequest)
	}

	reqURL, err := url.Parse(a.BaseURL)
	if err != nil {
		return BucketIsUniqueResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusInternalServerError)
	}

	reqURL.Path = path.Join(reqURL.Path, "/v1/bucket")
	// nolint: staticcheck
	reqURL.Query().Set("bucket", bucketName)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL.String(), nil)
	if err != nil {
		return BucketIsUniqueResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusInternalServerError)
	}
	logger.With("bucket name", bucketName, "base url", a.BaseURL, "token", a.Token, "reqURL", reqURL.String()).Debug("auth client check bucket")

	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Forwarded", "for="+clientIP)
	middleware.AddRequestIDToHeaders(req)

	delay := a.BackOff
	client := http.Client{
		Timeout:   a.Timeout,
		Transport: &http.Transport{ResponseHeaderTimeout: a.Timeout},
	}
	for {
		resp, err := client.Do(req)
		if err != nil {
			if !delay.Maxed() {
				if err := delay.Wait(ctx); err != nil {
					return BucketIsUniqueResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), errdata.HTTPStatusClientClosedRequest)
				}
				continue
			}
			return BucketIsUniqueResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusInternalServerError)
		}

		// Use an anonymous function for deferring the response close before the
		// next retry and not pilling it up when the method returns.
		retry, authResp, err := func() (retry bool, _ BucketIsUniqueResponse, _ error) {
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusInternalServerError {
				return true, BucketIsUniqueResponse{}, nil // auth only returns this for unexpected issues
			}

			if resp.StatusCode != http.StatusOK {
				return false, BucketIsUniqueResponse{}, errdata.WithStatus(AuthServiceError.New("%s", resp.Status), resp.StatusCode)
			}

			var bucketResp BucketIsUniqueResponse
			if err := json.NewDecoder(resp.Body).Decode(&bucketResp); err != nil {
				if !delay.Maxed() {
					return true, BucketIsUniqueResponse{}, nil
				}
				return false, BucketIsUniqueResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), http.StatusInternalServerError)
			}

			return false, bucketResp, nil
		}()

		if retry {
			if err := delay.Wait(ctx); err != nil {
				return BucketIsUniqueResponse{}, errdata.WithStatus(AuthServiceError.Wrap(err), errdata.HTTPStatusClientClosedRequest)
			}
			continue
		}

		return authResp, err
	}
}
