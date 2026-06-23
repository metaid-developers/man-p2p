package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestResponseSizeWriterCountsWriteAndWriteStringBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := &responseSizeWriter{ResponseWriter: ctx.Writer}

	if _, err := writer.Write([]byte("abc")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if _, err := writer.WriteString("defg"); err != nil {
		t.Fatalf("write string failed: %v", err)
	}

	if writer.bytesWritten != 7 {
		t.Fatalf("expected 7 bytes written, got %d", writer.bytesWritten)
	}
	if recorder.Body.String() != "abcdefg" {
		t.Fatalf("expected response body abcdefg, got %q", recorder.Body.String())
	}
}

func TestSlowRequestLoggerPreservesNoWriteHandlerResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SlowRequestLogger())
	router.GET("/empty", func(ctx *gin.Context) {})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/empty", nil)
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", recorder.Body.String())
	}
}

func TestSlowRequestLoggerLogsSlowRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
	})

	router := gin.New()
	router.Use(SlowRequestLogger())
	router.GET("/slow", func(ctx *gin.Context) {
		time.Sleep(slowRequestThreshold + 10*time.Millisecond)
		ctx.String(http.StatusAccepted, "slow body")
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/slow?ignored=true", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	router.ServeHTTP(recorder, req)

	logged := buf.String()
	for _, want := range []string{
		"slow request",
		"method=GET",
		"path=/slow",
		"status=202",
		"bytes=9",
		"client_ip=192.0.2.10",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("expected log to contain %q, got %q", want, logged)
		}
	}
}
