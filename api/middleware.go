package api

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

const slowRequestThreshold = 2 * time.Second

type responseSizeWriter struct {
	gin.ResponseWriter
	bytesWritten int
}

func (w *responseSizeWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.bytesWritten += n
	return n, err
}

func (w *responseSizeWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	w.bytesWritten += n
	return n, err
}

func SlowRequestLogger() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()
		writer := &responseSizeWriter{ResponseWriter: ctx.Writer}
		ctx.Writer = writer

		ctx.Next()

		elapsed := time.Since(start)
		if elapsed >= slowRequestThreshold {
			log.Printf("slow request method=%s path=%s status=%d bytes=%d elapsed=%s client_ip=%s",
				ctx.Request.Method,
				ctx.Request.URL.Path,
				ctx.Writer.Status(),
				writer.bytesWritten,
				elapsed.Round(time.Millisecond),
				ctx.ClientIP(),
			)
		}
	}
}
