package main

// Wails 2.9.3 uses UTType for file dialogs but does not link its
// framework; newer Xcode linkers no longer pull it in implicitly.

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
