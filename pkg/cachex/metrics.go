package cachex

import "sync/atomic"

type lookupObserver func(backend, result string)

var lookupObserverPointer atomic.Pointer[lookupObserver]

func SetLookupObserver(observer func(backend, result string)) {
	if observer == nil {
		lookupObserverPointer.Store(nil)
		return
	}
	wrapped := lookupObserver(observer)
	lookupObserverPointer.Store(&wrapped)
}

func observeLookup(backend, result string) {
	observer := lookupObserverPointer.Load()
	if observer != nil {
		(*observer)(backend, result)
	}
}
