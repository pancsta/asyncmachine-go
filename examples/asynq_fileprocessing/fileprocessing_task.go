// This example shows how to use AsyncMachine as an Asynq worker, which gives:
// - retries / scheduling
// - queue management
// - network distribution
// - and more, see https://github.com/hibiken/asynq
//
// For simplicity, we're using an existing FileProcessing example, which is a
// port of the Temporal one.
// See /examples/temporal-fileprocessing/fileprocessing.go
//
// Steps to run the example:
// 1. Start redis `docker run -p 6379:6379 --rm redis`
// 2. Enqueue `go run examples/asynq-fileprocessing/client/client.go`
// 3. Execute `go run examples/asynq-fileprocessing/worker/worker.go`
// 3. Inspect the result:
//   1. `go install github.com/hibiken/asynq/tools/asynq@latest`
//   2. `asynq dash`
//   3. Check default->Completed
//
// Sample output from the worker (LogLevel == LogChanges):
//
// asynq: pid=1355930 2024/01/05 08:55:26.460621 INFO: Starting processing
// asynq: pid=1355930 2024/01/05 08:55:26.460663 INFO: Send signal TSTP to stop processing new tasks
// asynq: pid=1355930 2024/01/05 08:55:26.460671 INFO: Send signal TERM or INT to terminate the process
// 2024/01/05 09:55:26 Processing file foo.txt
// 2024/01/05 09:55:26 [46206] [state] +DownloadingFile
// 2024/01/05 09:55:26 [46206] [external] Downloading file... foo.txt
// 2024/01/05 09:55:26 [46206] [state] +FileDownloaded -DownloadingFile
// 2024/01/05 09:55:26 [46206] [state:auto] +ProcessingFile
// 2024/01/05 09:55:26 waiting: DownloadingFile to FileUploaded
// 2024/01/05 09:55:26 [46206] [external] processFileActivity succeed /tmp/temporal_sample1517449749
// 2024/01/05 09:55:26 [46206] [state] +FileProcessed -ProcessingFile
// 2024/01/05 09:55:26 [46206] [external] cleanup /tmp/temporal_sample1133869176
// 2024/01/05 09:55:26 [46206] [state:auto] +UploadingFile
// 2024/01/05 09:55:26 [46206] [external] uploadFileActivity begin /tmp/temporal_sample1517449749
// 2024/01/05 09:55:26 [46206] [external] uploadFileActivity succeed /tmp/temporal_sample1517449749
// 2024/01/05 09:55:26 [46206] [state] +FileUploaded -UploadingFile
// 2024/01/05 09:55:26 [46206] [external] cleanup /tmp/temporal_sample1517449749

package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hibiken/asynq"

	processor "github.com/pancsta/asyncmachine-go/examples/temporal_fileprocessing"
)

const (
	TypeFileProcessing = "file:process"
)

type FileProcessingPayload struct {
	Filename string
}

func NewFileProcessingTask(filename string) (*asynq.Task, error) {
	payload, err := json.Marshal(FileProcessingPayload{Filename: filename})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeFileProcessing, payload, asynq.Retention(24*time.Hour)), nil
}

func HandleFileProcessingTask(ctx context.Context, t *asynq.Task) error {
	var p FileProcessingPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("json.Unmarshal failed: %v: %w", err, asynq.SkipRetry)
	}
	log.Printf("Processing file %s", p.Filename)
	machine, err := processor.FileProcessingFlow(ctx, log.Printf, p.Filename)
	if err != nil {
		return err
	}
	// save the machine state as the result
	ret := machine.String()
	if _, err := t.ResultWriter().Write([]byte(ret)); err != nil {
		return fmt.Errorf("failed to write task result: %v", err)
	}
	return nil
}
