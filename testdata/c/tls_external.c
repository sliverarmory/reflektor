#include <stdint.h>

extern __thread int external_tls;
static __thread int local_tls = 7;

uintptr_t ReflektorExternalTLS(void) {
	return (uintptr_t)(++local_tls + external_tls);
}
