#include <stdint.h>

#if defined(_WIN32)
#define REFLEKTOR_EXPORT __declspec(dllexport)
#else
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#endif

REFLEKTOR_EXPORT uintptr_t ReflektorCLIAdd(
    uintptr_t first,
    uintptr_t second,
    uintptr_t third) {
  return first + second * 10 + third * 100;
}

REFLEKTOR_EXPORT uintptr_t ReflektorCLIZero(void) {
  return 7;
}
