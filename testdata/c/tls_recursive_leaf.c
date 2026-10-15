#include <stdint.h>

static __thread int leaf_value = 7;

uintptr_t ReflektorTLSLeafNext(void) {
	return (uintptr_t)++leaf_value;
}
