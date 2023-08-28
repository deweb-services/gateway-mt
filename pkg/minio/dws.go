package minio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	"storj.io/minio/cmd"
	"storj.io/minio/pkg/bucket/policy"
)

const (
	Sep          = "/"
	VarKeyBucket = "bucket"
	VarKeyObject = "object"
)

const (
	nodeBucketPath = "/storage/bucket"
)

const (
	ErrAccessDenied        = "ErrAccessDenied"
	ErrInternalError       = "ErrInternalError"
	ErrBucketAlreadyExists = "ErrBucketAlreadyExists"
	ErrNoSuchBucket        = "ErrNoSuchBucket"
)

var apiErrors = map[string]cmd.APIError{
	ErrAccessDenied: {
		Code:           "AccessDenied",
		Description:    "Access Denied.",
		HTTPStatusCode: http.StatusForbidden,
	},
	ErrInternalError: {
		Code:           "InternalError",
		Description:    "We encountered an internal error, please try again.",
		HTTPStatusCode: http.StatusInternalServerError,
	},
	ErrBucketAlreadyExists: {
		Code: "BucketAlreadyExists",
		Description: "The requested bucket name is not available. The bucket " +
			"namespace is shared by all users of the system. Please select a different name and try again.",
		HTTPStatusCode: http.StatusConflict,
	},
	ErrNoSuchBucket: {
		Code:           "NoSuchBucket",
		Description:    "The specified bucket does not exist",
		HTTPStatusCode: http.StatusNotFound,
	},
}

type DwsConfig struct {
	UuidResolverAddr string `help:"full path to dws node service for resolving uuids" default:"localhost:6005"`
	DwsNodeToken     string `help:"dws node token" releaseDefault:"" default:"secret"`
}

func (h objectAPIHandlersWrapper) parseNodeHost() string {
	if h.nodeHost != "" {
		return h.nodeHost
	}
	u, err := url.Parse(h.uuidResolverHost)
	if err != nil {
		h.logger.With("error", err).Error("parse node host")
		return h.nodeHost
	}
	return u.Scheme + "://" + u.Host
}

func (h objectAPIHandlersWrapper) getUserID(r *http.Request, w http.ResponseWriter) (string, error) {
	ctx := cmd.NewContext(r, w, "")
	cred, _, _ := cmd.CheckRequestAuthTypeCredential(ctx, r, policy.HeadBucketAction, "", "")
	m := map[string]any{
		"accessKey": cred.AccessKey,
	}
	b, err := json.Marshal(m)
	if err != nil {
		h.logger.With("error", err).Error("marshal getUserID request")
		return "", fmt.Errorf("json marshall error: %w", err)
	}
	resp, err := h.httpClient.Post(h.uuidResolverHost, "application/json", bytes.NewBuffer(b))
	if err != nil {
		h.logger.With("error", err).Error("getUserID post request")
		return "", fmt.Errorf("http client post error: %w", err)
	}
	defer resp.Body.Close()
	ba, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.With("error", err).Error("getUserID read body")
		return "", fmt.Errorf("could not read http response, error: %w", err)
	}
	var res map[string]string
	if err := json.Unmarshal(ba, &res); err != nil {
		h.logger.With("error", err, "body", string(ba)).Error("getUserID unmarshall body")
		return "", fmt.Errorf("could not unmarshal http response, error: %w", err)
	}

	return res["uuid"], nil
}

func (h objectAPIHandlersWrapper) nodeBucketRequest(r *http.Request, method string, bucketName string) (int, error) {
	sc := 0
	u := h.nodeHost + nodeBucketPath
	var reader io.Reader
	headers := map[string]string{
		"Authorization": h.nodeToken,
	}
	switch method {
	case "HEAD", "DELETE":
		u = u + "/" + bucketName
	case "POST":
		type payload struct {
			Name string `json:"name"`
		}
		p := payload{Name: bucketName}
		headers["Content-Type"] = "application/json"
		ba, err := json.Marshal(p)
		if err != nil {
			h.logger.With("error", err).Error("nodeBucketRequest marshal request body")
			return sc, fmt.Errorf("marshal request body error: %w", err)
		}
		reader = bytes.NewBuffer(ba)
	default:
		h.logger.With("method", method).Error("nodeBucketRequest wrong method")
		return sc, fmt.Errorf("wrong http method: %s", method)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, u, reader)
	if err != nil {
		h.logger.With("error", err).Error("nodeBucketRequest new request")
		return sc, fmt.Errorf("could not create a request: %w", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.With("error", err).Error("nodeBucketRequest do request")
		return sc, fmt.Errorf("could not do request: %w", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (h objectAPIHandlersWrapper) bucketPrefixSubstitutionWithoutObject(w http.ResponseWriter, r *http.Request, fName string) error {
	ctx := cmd.NewContext(r, w, fName)
	vars := mux.Vars(r)
	userID, err := h.getUserID(r, w)
	if err != nil {
		cmd.WriteErrorResponse(ctx, w, apiErrors[ErrAccessDenied], r.URL, false)
		return fmt.Errorf("user not found")
	}
	vars[VarKeyBucket] = userID
	return nil
}

func (h objectAPIHandlersWrapper) bucketPrefixSubstitution(w http.ResponseWriter, r *http.Request, fName string) error {
	ctx := cmd.NewContext(r, w, fName)
	vars := mux.Vars(r)
	bucket := vars[VarKeyBucket]
	userID, err := h.getUserID(r, w)
	if err != nil {
		cmd.WriteErrorResponse(ctx, w, apiErrors[ErrAccessDenied], r.URL, false)
		return fmt.Errorf("user not found")
	}
	vars[VarKeyBucket] = userID
	vars[VarKeyObject] = bucket
	return nil
}

func (h objectAPIHandlersWrapper) objectPrefixSubstitution(w http.ResponseWriter, r *http.Request, fName string) error {
	ctx := cmd.NewContext(r, w, fName)
	vars := mux.Vars(r)
	bucket := vars[VarKeyBucket]
	object := vars[VarKeyObject]
	userID, err := h.getUserID(r, w)
	if err != nil {
		cmd.WriteErrorResponse(ctx, w, apiErrors[ErrAccessDenied], r.URL, false)
		return fmt.Errorf("user not found")
	}
	vars[VarKeyBucket] = userID
	vars[VarKeyObject] = strings.Join([]string{bucket, object}, Sep)
	return nil
}

type MockResponseWriter struct {
	code int
}

func (m *MockResponseWriter) Header() http.Header {
	return http.Header{}
}

func (m *MockResponseWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (m *MockResponseWriter) WriteHeader(statusCode int) {
	m.code = statusCode
}

func (m *MockResponseWriter) GetStatusCode() int {
	return m.code
}

type WrapperResponseWriter struct {
	http.ResponseWriter
	currentHeader int
}

func NewWrapperResponseWriter(w http.ResponseWriter) *WrapperResponseWriter {
	return &WrapperResponseWriter{
		ResponseWriter: w,
	}
}

func (w *WrapperResponseWriter) WriteHeader(statusCode int) {
	w.currentHeader = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *WrapperResponseWriter) getCurrentStatus() int {
	return w.currentHeader
}
