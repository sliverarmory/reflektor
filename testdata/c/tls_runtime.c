#include <pthread.h>
#include <stdint.h>

static __thread int initialized = 7;
static __thread int zero_fill;
static __thread int constructor_value = 10;

__attribute__((constructor)) static void initialize_tls(void) {
	++constructor_value;
}

uintptr_t ReflektorTLSConstructorValue(void) {
	return (uintptr_t)constructor_value;
}

uintptr_t ReflektorTLSNext(void) {
	return (uintptr_t)(++initialized * 100 + ++zero_fill);
}

static void *thread_entry(void *opaque) {
	uintptr_t *value = (uintptr_t *)opaque;
	*value = ReflektorTLSNext();
	return 0;
}

uintptr_t ReflektorTLSThreadIsolation(void) {
	pthread_t first, second;
	uintptr_t a = 0, b = 0;
	if (pthread_create(&first, 0, thread_entry, &a) != 0) return 0;
	if (pthread_create(&second, 0, thread_entry, &b) != 0) {
		pthread_join(first, 0);
		return 0;
	}
	pthread_join(first, 0);
	pthread_join(second, 0);
	return a == 801 && b == 801;
}
