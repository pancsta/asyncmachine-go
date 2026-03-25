//go:generate go run ./gen
package tinygo

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/pancsta/asyncmachine-go/examples/tinygo/states"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	// "github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

var ss = states.FileStates

type Logger func(msg string, args ...any)

// MachineHandlers is a struct of handlers & their data for the FileProcessing
// machine. None of the handlers can block.
type MachineHandlers struct {
	// default handler for the build in Exception state
	*am.ExceptionHandler
	BlobStore         *BlobStore
	Filename          string
	DownloadedName    string
	ProcessedFileName string
}

// DownloadingFileState is a _final_ entry handler for the DownloadingFile
// state.
func (h *MachineHandlers) DownloadingFileState(e *am.Event) {
	e.Machine().Log("Downloading file... %s", h.Filename)
	// read args
	h.Filename = e.Args["filename"].(string)
	// tick-based ctx
	stateCtx := e.Machine().NewStateCtx(ss.DownloadingFile)

	// unblock
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}
		data := h.BlobStore.downloadFile(h.Filename)

		tmpFile, err := saveToTmpFile(data)
		if err != nil {
			e.Machine().AddErr(err, nil)
			return
		}
		h.DownloadedName = tmpFile.Name()
		// done, next step
		e.Machine().Add1(ss.FileDownloaded, nil)
	}()
}

// ProcessingFileState is a _final_ entry handler for the ProcessingFile
// state.
func (h *MachineHandlers) ProcessingFileState(e *am.Event) {
	// tick-based ctx
	stateCtx := e.Machine().NewStateCtx(ss.ProcessingFile)

	// unblock
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}
		// read downloaded file
		data, err := os.ReadFile(h.DownloadedName)
		if err != nil {
			e.Machine().Log("processFileActivity failed to read file %s.",
				h.DownloadedName)
			e.Machine().AddErr(err, nil)
			return
		}
		// process the file
		transData := transcodeData(stateCtx, data)
		tmpFile, err := saveToTmpFile(transData)
		if err != nil {
			e.Machine().Log("processFileActivity failed to save tmp file.")
			e.Machine().AddErr(err, nil)
			return
		}

		h.ProcessedFileName = tmpFile.Name()
		e.Machine().Log("processFileActivity succeed %s", h.ProcessedFileName)
		// done, next step
		e.Machine().Add1(ss.FileProcessed, nil)
	}()
}

// ProcessingFileEnd is a _final_ exit handler for the ProcessingFile
// state.
func (h *MachineHandlers) ProcessingFileEnd(e *am.Event) {
	// clean up temp the file
	e.Machine().Log("cleanup %s", h.DownloadedName)
	_ = os.Remove(h.DownloadedName)
}

// UploadingFileState is a _final_ transition handler for the
// UploadingFile state.
func (h *MachineHandlers) UploadingFileState(e *am.Event) {
	// tick-based ctx
	stateCtx := e.Machine().NewStateCtx(ss.UploadingFile)

	// unblock
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}
		e.Machine().Log("uploadFileActivity begin %s", h.ProcessedFileName)
		err := h.BlobStore.uploadFile(stateCtx, h.ProcessedFileName)
		if err != nil {
			e.Machine().Log("uploadFileActivity uploading failed.")
			e.Machine().AddErr(err, nil)
			return
		}
		e.Machine().Log("uploadFileActivity succeed %s", h.ProcessedFileName)
		// done, next step
		e.Machine().Add1(ss.FileUploaded, nil)
	}()
}

// UploadingFileEnd is a _final_ exit handler for the UploadingFile
// state.
func (h *MachineHandlers) UploadingFileEnd(e *am.Event) {
	// clean up temp the file
	e.Machine().Log("cleanup %s", h.ProcessedFileName)
	_ = os.Remove(h.ProcessedFileName)
}

// FileProcessingFlow is an example of how to use the FileProcessing machine.
func FileProcessingFlow(ctx context.Context, log Logger, filename string) (*am.Machine, error) {
	// init
	h := &MachineHandlers{}
	mach, err := am.NewCommon(ctx, "tinygo", states.FileSchema, ss.Names(), nil, nil, &am.Opts{
		DontLogId: true,
	})
	if err != nil {
		return nil, err
	}
	if err = BindMachineHandlers(h, mach); err != nil {
		return nil, err
	}
	// TODO
	// amhelp.MachDebugEnv(mach)

	// different log granularity and a custom output
	mach.SemLogger().SetLevel(am.LogChanges)
	// machine.SemLogger().SetLevel(am.LogOps)
	// machine.SemLogger().SetLevel(am.LogDecisions)
	// machine.SemLogger().SetLevel(am.LogEverything)
	mach.SemLogger().SetLogger(func(l am.LogLevel, msg string, args ...any) {
		log(msg, args...)
	})

	// start it up!
	mach.Add1(ss.DownloadingFile, am.A{"filename": filename})

	// DownloadingFile to FileUploaded
	log("waiting: DownloadingFile to FileUploaded")
	select {
	case <-time.After(5 * time.Second):
		return mach, errors.New("timeout")
	case <-mach.WhenErr(nil):
		return mach, mach.Err()
	case <-mach.When1(ss.FileUploaded, nil):
	}

	return mach, nil
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
