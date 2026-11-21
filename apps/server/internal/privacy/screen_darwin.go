package privacy

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include <CoreGraphics/CoreGraphics.h>
#include <ApplicationServices/ApplicationServices.h>
*/
import "C"

func screenRecording() bool { return bool(C.CGPreflightScreenCaptureAccess()) }

func accessibility() bool { return C.AXIsProcessTrusted() != 0 }
