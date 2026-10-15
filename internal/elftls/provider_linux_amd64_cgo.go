//go:build linux && amd64 && cgo

package elftls

/*
#cgo LDFLAGS: -pthread
#include <errno.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct reflektor_tls_module {
	uintptr_t id;
	size_t size;
	size_t filesz;
	size_t alignment;
	unsigned char *initial;
	struct reflektor_tls_module *next;
} reflektor_tls_module;

typedef struct reflektor_tls_block {
	uintptr_t id;
	size_t size;
	unsigned char *data;
	struct reflektor_tls_block *next;
} reflektor_tls_block;

typedef struct {
	uintptr_t module;
	uintptr_t offset;
} reflektor_tls_index;

static pthread_mutex_t reflektor_tls_lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_once_t reflektor_tls_once = PTHREAD_ONCE_INIT;
static pthread_key_t reflektor_tls_key;
static int reflektor_tls_key_error;
static uintptr_t reflektor_tls_next_id = 1;
static reflektor_tls_module *reflektor_tls_modules;

static void reflektor_tls_thread_destroy(void *opaque) {
	reflektor_tls_block *block = (reflektor_tls_block *)opaque;
	while (block != NULL) {
		reflektor_tls_block *next = block->next;
		free(block->data);
		free(block);
		block = next;
	}
}

static void reflektor_tls_create_key(void) {
	reflektor_tls_key_error = pthread_key_create(&reflektor_tls_key, reflektor_tls_thread_destroy);
}

// The ID is private to Reflektor's resolver; it is never handed to libc's DTV.
// IDs are not reused, so TLS cached on a long-lived thread cannot alias a
// later load at the same slot. Release frees the shared template; thread blocks
// are reclaimed by the pthread-key destructor when each thread exits.
static int reflektor_tls_register(const unsigned char *initial, size_t filesz,
		size_t memsz, size_t alignment, uintptr_t *id) {
	int err = pthread_once(&reflektor_tls_once, reflektor_tls_create_key);
	if (err != 0) return err;
	if (reflektor_tls_key_error != 0) return reflektor_tls_key_error;
	if (memsz == 0 || filesz > memsz || id == NULL) return EINVAL;

	reflektor_tls_module *module = (reflektor_tls_module *)calloc(1, sizeof(*module));
	if (module == NULL) return ENOMEM;
	module->initial = (unsigned char *)malloc(filesz == 0 ? 1 : filesz);
	if (module->initial == NULL) {
		free(module);
		return ENOMEM;
	}
	if (filesz != 0) memcpy(module->initial, initial, filesz);
	module->size = memsz;
	module->filesz = filesz;
	module->alignment = alignment < sizeof(void *) ? sizeof(void *) : alignment;

	pthread_mutex_lock(&reflektor_tls_lock);
	if (reflektor_tls_next_id == 0) {
		pthread_mutex_unlock(&reflektor_tls_lock);
		free(module->initial);
		free(module);
		return EOVERFLOW;
	}
	module->id = reflektor_tls_next_id++;
	module->next = reflektor_tls_modules;
	reflektor_tls_modules = module;
	*id = module->id;
	pthread_mutex_unlock(&reflektor_tls_lock);
	return 0;
}

static void reflektor_tls_release(uintptr_t id) {
	pthread_mutex_lock(&reflektor_tls_lock);
	reflektor_tls_module **cursor = &reflektor_tls_modules;
	while (*cursor != NULL && (*cursor)->id != id) cursor = &(*cursor)->next;
	reflektor_tls_module *module = *cursor;
	if (module != NULL) *cursor = module->next;
	pthread_mutex_unlock(&reflektor_tls_lock);
	if (module != NULL) {
		free(module->initial);
		free(module);
	}
}

static void *reflektor_tls_get_addr(const reflektor_tls_index *index) {
	if (index == NULL) abort();
	reflektor_tls_block *head = (reflektor_tls_block *)pthread_getspecific(reflektor_tls_key);
	for (reflektor_tls_block *block = head; block != NULL; block = block->next) {
		if (block->id == index->module) {
			if (index->offset >= block->size) abort();
			return block->data + index->offset;
		}
	}

	pthread_mutex_lock(&reflektor_tls_lock);
	reflektor_tls_module *module = reflektor_tls_modules;
	while (module != NULL && module->id != index->module) module = module->next;
	if (module == NULL || index->offset >= module->size) {
		pthread_mutex_unlock(&reflektor_tls_lock);
		abort();
	}
	reflektor_tls_block *block = (reflektor_tls_block *)calloc(1, sizeof(*block));
	if (block == NULL || posix_memalign((void **)&block->data, module->alignment, module->size) != 0) {
		pthread_mutex_unlock(&reflektor_tls_lock);
		abort();
	}
	memset(block->data, 0, module->size);
	memcpy(block->data, module->initial, module->filesz);
	block->size = module->size;
	pthread_mutex_unlock(&reflektor_tls_lock);
	block->id = index->module;
	block->next = head;
	if (pthread_setspecific(reflektor_tls_key, block) != 0) abort();
	return block->data + index->offset;
}

static uintptr_t reflektor_tls_resolver_pointer(void) {
	return (uintptr_t)&reflektor_tls_get_addr;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func available() bool { return true }

func register(initial []byte, size uint64, align uint64) (uint64, error) {
	var ptr *C.uchar
	if len(initial) != 0 {
		ptr = (*C.uchar)(unsafe.Pointer(&initial[0]))
	}
	var id C.uintptr_t
	if status := C.reflektor_tls_register(ptr, C.size_t(len(initial)), C.size_t(size), C.size_t(align), &id); status != 0 {
		return 0, fmt.Errorf("register general PT_TLS: pthread status %d", int(status))
	}
	return uint64(id), nil
}

func resolverAddress() uint64 { return uint64(C.reflektor_tls_resolver_pointer()) }

func release(id uint64) { C.reflektor_tls_release(C.uintptr_t(id)) }
