package privacy

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

func screenRecording() bool { return bool(C.CGPreflightScreenCaptureAccess()) }
