package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestConvertHTTPChunksEndsWithoutReaderWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := convertHTTPChunks(ctx, make(chan pluginapi.HTTPStreamChunk))
	cancel()
	// A send after cancel would block forever once the ABI pump stopped reading.
	if chunk, ok := <-out; ok {
		t.Fatalf("chunk after cancel = %#v, want closed stream", chunk)
	}
}

func TestConvertHTTPChunksEndsAtUpstreamError(t *testing.T) {
	in := make(chan pluginapi.HTTPStreamChunk, 2)
	in <- pluginapi.HTTPStreamChunk{Err: errors.New("upstream reset")}
	in <- pluginapi.HTTPStreamChunk{Payload: []byte(`data: {}`)}
	close(in)
	var chunks []pluginapi.ExecutorStreamChunk
	for chunk := range convertHTTPChunks(context.Background(), in) {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 1 || chunks[0].Err == nil {
		t.Fatalf("chunks = %#v, want only the error", chunks)
	}
}
