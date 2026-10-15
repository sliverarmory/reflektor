#include <pthread.h>
#include <stdint.h>

extern uintptr_t ReflektorTLSLeafNext(void);

static __thread int root_value = 3;

uintptr_t ReflektorTLSRootNext(void) {
	return (uintptr_t)(++root_value * 100 + ReflektorTLSLeafNext());
}

static void *thread_entry(void *opaque) {
	uintptr_t *value = (uintptr_t *)opaque;
	*value = ReflektorTLSRootNext();
	return 0;
}

uintptr_t ReflektorTLSRecursiveThreadIsolation(void) {
	pthread_t first, second;
	uintptr_t a = 0, b = 0;
	if (pthread_create(&first, 0, thread_entry, &a) != 0) return 0;
	if (pthread_create(&second, 0, thread_entry, &b) != 0) {
		pthread_join(first, 0);
		return 0;
	}
	pthread_join(first, 0);
	pthread_join(second, 0);
	return a == 408 && b == 408;
}
