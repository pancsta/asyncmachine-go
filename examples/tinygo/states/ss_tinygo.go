//go:build !tinygo

//go:generate go run ./gen
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// S is a type alias for a list of state names.
type S = am.S

// FileStatesDef contains states for file processing.
type FileStatesDef struct {
	*am.StatesBase
	DownloadingFile string
	FileDownloaded  string
	ProcessingFile  string
	FileProcessed   string
	UploadingFile   string
	FileUploaded    string
}

// FileGroupsDef contains all the state groups of the File state schema.
type FileGroupsDef struct {
	Downloaded S
	Processed  S
	Uploaded   S
}

// FileSchema represents all relations and properties of FileStates.
var FileSchema = am.Schema{
	ssF.DownloadingFile: {Remove: sgF.Downloaded},
	ssF.FileDownloaded:  {Remove: sgF.Downloaded},
	ssF.ProcessingFile: {
		Auto:    true,
		Require: S{ssF.FileDownloaded},
		Remove:  sgF.Processed,
	},
	ssF.FileProcessed: {Remove: sgF.Processed},
	ssF.UploadingFile: {
		Auto:    true,
		Require: S{ssF.FileProcessed},
		Remove:  sgF.Uploaded,
	},
	ssF.FileUploaded: {Remove: sgF.Uploaded},
}

// EXPORTS AND GROUPS

var (
	ssF = am.NewStates(FileStatesDef{})
	sgF = am.NewStateGroups(FileGroupsDef{
		Downloaded: S{ssF.DownloadingFile, ssF.FileDownloaded},
		Processed:  S{ssF.ProcessingFile, ssF.FileProcessed},
		Uploaded:   S{ssF.UploadingFile, ssF.FileUploaded},
	})

	// FileStates contains all the states for the File state schema.
	FileStates = ssF

	// FileGroups contains all the state groups for the File state schema.
	FileGroups = sgF
)
