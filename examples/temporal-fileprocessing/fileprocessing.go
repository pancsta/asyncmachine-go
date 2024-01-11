// Based on https://github.com/temporalio/samples-go/blob/main/fileprocessing/
//
// Go playground: https://goplay.tools/snippet/aTo4hsyJZck
//
// This example shows a simple file processing workflow going from
// DownloadingFile to FileUploaded.
//
// Sample output (LogLevel == LogChanges):
//
// === RUN   TestFileProcessing
//     fileprocessing.go:164: [05a4d] [state] +DownloadingFile
//     fileprocessing.go:164: [05a4d] [external] Downloading file... foo.txt
//     fileprocessing.go:178: waiting: DownloadingFile to FileUploaded
//     fileprocessing.go:164: [05a4d] [state] +FileDownloaded -DownloadingFile
//     fileprocessing.go:164: [05a4d] [state:auto] +ProcessingFile
//     fileprocessing.go:164: [05a4d] [external] processFileActivity succeed /tmp/temporal_sample1737998348
//     fileprocessing.go:164: [05a4d] [state] +FileProcessed -ProcessingFile
//     fileprocessing.go:164: [05a4d] [external] cleanup /tmp/temporal_sample3389363275
//     fileprocessing.go:164: [05a4d] [state:auto] +UploadingFile
//     fileprocessing.go:164: [05a4d] [external] uploadFileActivity begin /tmp/temporal_sample1737998348
//     fileprocessing.go:164: [05a4d] [external] uploadFileActivity succeed /tmp/temporal_sample1737998348
//     fileprocessing.go:164: [05a4d] [state] +FileUploaded -UploadingFile
//     fileprocessing.go:164: [05a4d] [external] cleanup /tmp/temporal_sample1737998348
//     fileprocessing_test.go:19:
//         (FileUploaded:1 FileProcessed:1 FileDownloaded:1)
//     fileprocessing_test.go:20:
//         (FileDownloaded:1 FileProcessed:1 FileUploaded:1)[Exception:0 DownloadingFile:1 ProcessingFile:1 UploadingFile:1]
//     fileprocessing_test.go:21:
//         Exception:
//           State:   false 0
//
//         DownloadingFile:
//           State:   false 1
//           Remove:  FileDownloaded
//
//         FileDownloaded:
//           State:   true 1
//           Remove:  DownloadingFile
//
//         ProcessingFile:
//           State:   false 1
//           Auto:    true
//           Require: FileDownloaded
//           Remove:  FileProcessed
//
//         FileProcessed:
//           State:   true 1
//           Remove:  ProcessingFile
//
//         UploadingFile:
//           State:   false 1
//           Auto:    true
//           Require: FileProcessed
//           Remove:  FileUploaded
//
//         FileUploaded:
//           State:   true 1
//           Remove:  UploadingFile
//
//
// --- PASS: TestFileProcessing (0.20s)
// PASS

package temporal_fileprocessing

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type Logger func(msg string, args ...any)

// NewMachine creates a new FileProcessing machine with a DownloadingFile to
// FileUploaded flow.
func NewMachine(ctx context.Context) *am.Machine {
	// define states
	return am.New(ctx, am.States{
		// DownloadFileActivity
		"DownloadingFile": {
			Remove: am.S{"FileDownloaded"},
		},
		"FileDownloaded": {
			Remove: am.S{"DownloadingFile"},
		},
		// ProcessFileActivity
		"ProcessingFile": {
			Auto:    true,
			Require: am.S{"FileDownloaded"},
			Remove:  am.S{"FileProcessed"},
		},
		"FileProcessed": {
			Remove: am.S{"ProcessingFile"},
		},
		// UploadFileActivity
		"UploadingFile": {
			Auto:    true,
			Require: am.S{"FileProcessed"},
			Remove:  am.S{"FileUploaded"},
		},
		"FileUploaded": {
			Remove: am.S{"UploadingFile"},
		},
	}, nil)
}

// MachineHandlers is a struct of handlers & their data for the FileProcessing
// machine. None of the handlers can block.
type MachineHandlers struct {
	// default handler for the build in Exception state
	am.ExceptionHandler
	BlobStore         *BlobStore
	Filename          string
	DownloadedName    string
	ProcessedFileName string
}

// DownloadingFileState is a _final_ entry handler for the DownloadingFile
// state.
func (h *MachineHandlers) DownloadingFileState(e *am.Event) {
	// args definition
	h.Filename = e.Args["filename"].(string)

	e.Machine.Log("Downloading file... %s", h.Filename)
	// never block in a handler, always "go func" it
	go func() {
		data := h.BlobStore.downloadFile(h.Filename)

		tmpFile, err := saveToTmpFile(data)
		if err != nil {
			e.Machine.AddErr(err)
			return
		}
		h.DownloadedName = tmpFile.Name()
		// done, next step
		e.Machine.Add(am.S{"FileDownloaded"}, nil)
	}()
}

// ProcessingFileState is a _final_ entry handler for the ProcessingFile
// state.
func (h *MachineHandlers) ProcessingFileState(e *am.Event) {
	// Never block in a handler, always "go func" it.
	// State context will confirm that processing should still be happening.
	// Using machine's context directly will conflict with retry logic (if any).
	stateCtx := e.Machine.GetStateCtx("ProcessingFile")
	go func() {
		// read downloaded file
		data, err := os.ReadFile(h.DownloadedName)
		if err != nil {
			e.Machine.Log("processFileActivity failed to read file %s.",
				h.DownloadedName)
			e.Machine.AddErr(err)
			return
		}
		// process the file
		transData := transcodeData(stateCtx, data)
		tmpFile, err := saveToTmpFile(transData)
		if err != nil {
			e.Machine.Log("processFileActivity failed to save tmp file.")
			e.Machine.AddErr(err)
			return
		}

		h.ProcessedFileName = tmpFile.Name()
		e.Machine.Log("processFileActivity succeed %s", h.ProcessedFileName)
		// done, next step
		e.Machine.Add(am.S{"FileProcessed"}, nil)
	}()
}

// ProcessingFileEnd is a _final_ exit handler for the ProcessingFile
// state.
func (h *MachineHandlers) ProcessingFileEnd(e *am.Event) {
	// clean up temp the file
	e.Machine.Log("cleanup %s", h.DownloadedName)
	_ = os.Remove(h.DownloadedName)
}

// UploadingFileState is a _final_ transition handler for the
// UploadingFile state.
func (h *MachineHandlers) UploadingFileState(e *am.Event) {
	// Never block in a handler, always "go func" it.
	// State context will confirm that uploading should still be happening.
	// Using machine's context directly will conflict with retry logic (if any).
	stateCtx := e.Machine.GetStateCtx("ProcessingFile")
	go func() {
		e.Machine.Log("uploadFileActivity begin %s", h.ProcessedFileName)
		err := h.BlobStore.uploadFile(stateCtx, h.ProcessedFileName)
		if err != nil {
			e.Machine.Log("uploadFileActivity uploading failed.")
			e.Machine.AddErr(err)
			return
		}
		e.Machine.Log("uploadFileActivity succeed %s", h.ProcessedFileName)
		// done, next step
		e.Machine.Add(am.S{"FileUploaded"}, nil)
	}()
}

// UploadingFileEnd is a _final_ exit handler for the UploadingFile
// state.
func (h *MachineHandlers) UploadingFileEnd(e *am.Event) {
	// clean up temp the file
	e.Machine.Log("cleanup %s", h.ProcessedFileName)
	_ = os.Remove(h.ProcessedFileName)
}

// FileProcessingFlow is an example of how to use the FileProcessing machine.
func FileProcessingFlow(ctx context.Context, log Logger, filename string) (*am.Machine, error) {
	// init
	machine := NewMachine(ctx)

	// different log granularity and a custom output
	machine.SetLogLevel(am.LogChanges)
	// machine.SetLogLevel(am.LogOps)
	// machine.SetLogLevel(am.LogDecisions)
	// machine.SetLogLevel(am.LogEverything)
	machine.SetLogger(func(l am.LogLevel, msg string, args ...any) {
		log(msg, args...)
	})

	// bind handlers and wait for Ready
	binding, err := machine.BindHandlers(&MachineHandlers{})
	if err != nil {
		return machine, err
	}
	<-binding.Ready

	// start it up!
	machine.Add(am.S{"DownloadingFile"}, am.A{"filename": filename})

	// DownloadingFile to FileUploaded
	log("waiting: DownloadingFile to FileUploaded")
	select {
	case <-time.After(5 * time.Second):
		return machine, errors.New("timeout")
	case <-machine.WhenErr(nil):
		return machine, machine.Err
	case <-machine.When(am.S{"FileUploaded"}, nil):
	}

	return machine, nil
}

// Helpers (taken from the Temporal sample)

type BlobStore struct{}

func (b *BlobStore) downloadFile(fileID string) []byte {
	// dummy downloader
	dummyContent := "dummy content for fileID:" + fileID
	return []byte(dummyContent)
}

func (b *BlobStore) uploadFile(_ context.Context, filename string) error {
	// dummy uploader
	_, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	return nil
}

func transcodeData(_ context.Context, data []byte) []byte {
	// dummy file processor, just do upper case for the data.
	// in real world case, you would want to avoid load entire file content into
	// memory at once.
	time.Sleep(100 * time.Millisecond)
	return []byte(strings.ToUpper(string(data)))
}

func saveToTmpFile(data []byte) (f *os.File, err error) {
	tmpFile, err := os.CreateTemp("", "temporal_sample")
	if err != nil {
		return nil, err
	}
	_, err = tmpFile.Write(data)
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return nil, err
	}

	return tmpFile, nil
}
