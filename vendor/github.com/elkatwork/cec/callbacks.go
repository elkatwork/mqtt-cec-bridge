package cec

// #include <libcec/cecc.h>
import "C"

import (
	"unsafe"
)

var logChan = make(chan string)

//export logMessageCallback
func logMessageCallback(c unsafe.Pointer, msg *C.cec_log_message) C.int {
	//log.Println(C.GoString(msg.message))
	logChan <- C.GoString(msg.message)

	return 0
}
