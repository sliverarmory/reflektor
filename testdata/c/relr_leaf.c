#include <stdint.h>

#if defined(_WIN32)
#define REFLEKTOR_EXPORT __declspec(dllexport)
#else
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#endif

static uintptr_t relr_value = 41;

// Exporting the pointer array keeps its relative relocations in the image.
REFLEKTOR_EXPORT uintptr_t *reflektor_relr_pointers[8] = {
    &relr_value, &relr_value, &relr_value, &relr_value,
    &relr_value, &relr_value, &relr_value, &relr_value,
};

REFLEKTOR_EXPORT uintptr_t ReflektorRELRValue(void) {
  return *reflektor_relr_pointers[0];
}
