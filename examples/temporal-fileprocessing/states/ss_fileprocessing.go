package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Struct{
	DownloadingFile: {Remove: GroupFileDownloaded},
	FileDownloaded:  {Remove: GroupFileDownloaded},
	ProcessingFile: {
		Auto:    true,
		Require: S{FileDownloaded},
		Remove:  GroupFileProcessed,
	},
	FileProcessed: {Remove: GroupFileProcessed},
	UploadingFile: {
		Auto:    true,
		Require: S{FileProcessed},
		Remove:  GroupFileUploaded,
	},
	FileUploaded: {Remove: GroupFileUploaded},
}

// Groups of mutually exclusive states.

var (
	GroupFileDownloaded = S{DownloadingFile, FileDownloaded}
	GroupFileProcessed  = S{ProcessingFile, FileProcessed}
	GroupFileUploaded   = S{UploadingFile, FileUploaded}
)

//#region boilerplate defs

// Names of all the states (pkg enum).

const (
	DownloadingFile = "DownloadingFile"
	FileDownloaded  = "FileDownloaded"
	ProcessingFile  = "ProcessingFile"
	FileProcessed   = "FileProcessed"
	UploadingFile   = "UploadingFile"
	FileUploaded    = "FileUploaded"
)

// Names is an ordered list of all the state names.
var Names = S{DownloadingFile, FileDownloaded, ProcessingFile, FileProcessed, UploadingFile, FileUploaded}

//#endregion
