package minio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/gorilla/mux"
	"storj.io/minio/cmd"
	"storj.io/minio/pkg/bucket/policy"
)

const (
	Sep                = "/"
	VarKeyBucket       = "bucket"
	VarKeyObject       = "object"
	bucketResolverPath = "v1/bucket?bucket="
)

const (
	ErrAccessDenied        = "ErrAccessDenied"
	ErrInternalError       = "ErrInternalError"
	ErrBucketAlreadyExists = "ErrBucketAlreadyExists"
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
}

type BucketUniqueResolverResponse struct {
	Error       string `json:"error,omitempty"`
	IsAvailable bool   `json:"is_available,omitempty"`
}

func (h objectAPIHandlersWrapper) checkBucketExistence(r *http.Request) bool {
	w := &MockResponseWriter{}
	h.core.HeadBucketHandler(w, r)
	return w.GetStatusCode() == http.StatusOK
}

func (h objectAPIHandlersWrapper) getUserID(r *http.Request, w http.ResponseWriter) (string, error) {
	ctx := cmd.NewContext(r, w, "")
	cred, _, _ := cmd.CheckRequestAuthTypeCredential(ctx, r, policy.HeadBucketAction, "", "")
	m := map[string]any{
		"accessKey": cred.AccessKey,
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("json marshall error: %w", err)
	}
	resp, err := h.httpClient.Post(h.uuidResolverHost, "application/json", bytes.NewBuffer(b))
	if err != nil {
		return "", fmt.Errorf("http client post error: %w", err)
	}
	defer resp.Body.Close()
	ba, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("could not read http response, error: %w", err)
	}
	var res map[string]string
	if err := json.Unmarshal(ba, &res); err != nil {
		return "", fmt.Errorf("could not unmarshal http response, error: %w", err)
	}

	return res["uuid"], nil
}

func (h objectAPIHandlersWrapper) bucketNameIsAvailable(r *http.Request) (bool, error) {
	funcName := "bucketNameIsAvailable"
	vars := mux.Vars(r)
	bucket := vars[VarKeyBucket]
	if bucket == "" {
		return false, nil
	}
	resp, err := h.httpClient.Get(path.Join(h.bucketResolverHost, bucketResolverPath) + bucket)
	if err != nil {
		return false, fmt.Errorf("function: %s, could not get response from bucket resolver: %w", funcName, err)
	}
	defer resp.Body.Close()
	ba, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("function: %s, could not read response: %w", funcName, err)
	}
	var res BucketUniqueResolverResponse
	if err := json.Unmarshal(ba, &res); err != nil {
		return false, fmt.Errorf("function: %s, could not unmarshall response: %w", funcName, err)
	}
	if res.Error != "" {
		return false, fmt.Errorf("function: %s, error: %s", funcName, res.Error)
	}
	return res.IsAvailable, nil
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
