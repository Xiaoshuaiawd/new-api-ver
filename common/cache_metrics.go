package common

import "sync/atomic"

type cacheLookupObserver func(backend, result string)

var cacheLookupObserverPointer atomic.Pointer[cacheLookupObserver]

func SetCacheLookupObserver(observer func(backend, result string)) {
	if observer == nil {
		cacheLookupObserverPointer.Store(nil)
		return
	}
	wrapped := cacheLookupObserver(observer)
	cacheLookupObserverPointer.Store(&wrapped)
}

func observeCacheLookup(backend, result string) {
	observer := cacheLookupObserverPointer.Load()
	if observer != nil {
		(*observer)(backend, result)
	}
}
