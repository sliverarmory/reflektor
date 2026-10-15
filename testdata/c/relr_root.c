#include <stdint.h>

#if defined(_WIN32)
#define REFLEKTOR_EXPORT __declspec(dllexport)
#else
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#endif

extern uintptr_t ReflektorRELRValue(void);

static uintptr_t root_addend = 1;
REFLEKTOR_EXPORT uintptr_t *reflektor_relr_root_pointer = &root_addend;

REFLEKTOR_EXPORT uintptr_t ReflektorRELRRootValue(void) {
  return ReflektorRELRValue() + *reflektor_relr_root_pointer;
}
