//go:build darwin && cgo

package mcp

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>
static CFMutableDictionaryRef query(const char *service,const char *account) {
 CFMutableDictionaryRef d=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef s=CFStringCreateWithCString(NULL,service,kCFStringEncodingUTF8),a=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(d,kSecClass,kSecClassGenericPassword);CFDictionarySetValue(d,kSecAttrService,s);CFDictionarySetValue(d,kSecAttrAccount,a);CFRelease(s);CFRelease(a);return d;
}
static int secret_get(const char*s,const char*a,void **out,long *size){CFMutableDictionaryRef q=query(s,a);CFDictionarySetValue(q,kSecReturnData,kCFBooleanTrue);CFDictionarySetValue(q,kSecMatchLimit,kSecMatchLimitOne);CFTypeRef value=NULL;OSStatus code=SecItemCopyMatching(q,&value);CFRelease(q);if(code==errSecSuccess){*size=CFDataGetLength((CFDataRef)value);if(*size<=0||*size>(256<<10)){CFRelease(value);return errSecDecode;}*out=malloc(*size);if(!*out){CFRelease(value);return errSecAllocate;}memcpy(*out,CFDataGetBytePtr((CFDataRef)value),*size);CFRelease(value);}return code;}
static int secret_put(const char*s,const char*a,const void*b,long n){CFMutableDictionaryRef q=query(s,a);CFDataRef value=CFDataCreate(NULL,b,n);CFMutableDictionaryRef attrs=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);CFDictionarySetValue(attrs,kSecValueData,value);OSStatus code=SecItemUpdate(q,attrs);if(code==errSecItemNotFound){CFDictionarySetValue(q,kSecValueData,value);code=SecItemAdd(q,NULL);}CFRelease(attrs);CFRelease(value);CFRelease(q);return code;}
static int secret_delete(const char*s,const char*a){CFMutableDictionaryRef q=query(s,a);OSStatus code=SecItemDelete(q);CFRelease(q);return code;}
*/
import "C"
import (
	"context"
	"fmt"
	"unsafe"
)

type KeychainStore struct{ Service string }

func NewKeychainStore(service string) SecretStore { return KeychainStore{Service: service} }
func (k KeychainStore) Get(ctx context.Context, key string) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	s, a := C.CString(k.Service), C.CString(key)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(a))
	var ptr unsafe.Pointer
	var size C.long
	code := C.secret_get(s, a, &ptr, &size)
	if code == -25300 {
		return nil, ErrNoCredential
	}
	if code != 0 {
		return nil, fmt.Errorf("Keychain read failed (%d)", code)
	}
	defer C.free(ptr)
	if size > 256<<10 {
		return nil, fmt.Errorf("Keychain credential exceeds limit")
	}
	return C.GoBytes(ptr, C.int(size)), nil
}
func (k KeychainStore) Put(ctx context.Context, key string, b []byte) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if len(b) == 0 || len(b) > 256<<10 {
		return fmt.Errorf("invalid credential size")
	}
	s, a := C.CString(k.Service), C.CString(key)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(a))
	code := C.secret_put(s, a, unsafe.Pointer(&b[0]), C.long(len(b)))
	if code != 0 {
		return fmt.Errorf("Keychain write failed (%d)", code)
	}
	return nil
}
func (k KeychainStore) Delete(ctx context.Context, key string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	s, a := C.CString(k.Service), C.CString(key)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(a))
	code := C.secret_delete(s, a)
	if code != 0 && code != -25300 {
		return fmt.Errorf("Keychain delete failed (%d)", code)
	}
	return nil
}
