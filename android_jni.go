//go:build android && cgo

package main

/*
#include <jni.h>
#include <stdlib.h>

static inline const char *wr_chars(JNIEnv *env, jstring str) {
	return (*env)->GetStringUTFChars(env, str, NULL);
}
static inline void wr_release(JNIEnv *env, jstring str, const char *chars) {
	(*env)->ReleaseStringUTFChars(env, str, chars);
}
static inline jstring wr_string(JNIEnv *env, const char *chars) {
	return (*env)->NewStringUTF(env, chars);
}
static inline void wr_ready(JNIEnv *env, jobject receiver, const char *link) {
	jclass cls = (*env)->GetObjectClass(env, receiver);
	jmethodID method = (*env)->GetMethodID(env, cls, "onReady", "(Ljava/lang/String;)V");
	if (method != NULL) {
		jstring value = wr_string(env, link);
		if (value != NULL) {
			(*env)->CallVoidMethod(env, receiver, method, value);
			(*env)->DeleteLocalRef(env, value);
		}
	}
	(*env)->DeleteLocalRef(env, cls);
}
*/
import "C"

import (
	"runtime"
	"time"
	"unsafe"
)

func javaString(env *C.JNIEnv, input C.jstring) string {
	value := C.wr_chars(env, input)
	if value == nil {
		return ""
	}
	defer C.wr_release(env, input, value)
	return C.GoString(value)
}

//export Java_cn_wordreview_app_LocalEngine_serve
func Java_cn_wordreview_app_LocalEngine_serve(env *C.JNIEnv, receiver C.jobject, directory, zone C.jstring) C.jstring {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if location, err := time.LoadLocation(javaString(env, zone)); err == nil {
		time.Local = location
	}
	dir := javaString(env, directory)
	err := runEngine(true, dir, func(link string) {
		cString := C.CString(link)
		defer C.free(unsafe.Pointer(cString))
		C.wr_ready(env, receiver, cString)
	})
	if err == nil {
		return 0
	}
	message := C.CString(err.Error())
	defer C.free(unsafe.Pointer(message))
	return C.wr_string(env, message)
}
