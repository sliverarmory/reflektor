#include <stdint.h>

#if defined(__GNUC__)
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#else
#define REFLEKTOR_EXPORT
#endif

#define REFLEKTOR_CONSTRUCTOR_STATE ((uintptr_t)0x13579bdfu)
#define REFLEKTOR_DESTRUCTOR_SENTINEL ((uintptr_t)0x5a17c0deu)

static uintptr_t reflektor_lifecycle_state;
static uintptr_t* reflektor_lifecycle_output;

__attribute__((constructor)) static void reflektor_lifecycle_init(void) {
  reflektor_lifecycle_state = REFLEKTOR_CONSTRUCTOR_STATE;
}

__attribute__((destructor)) static void reflektor_lifecycle_fini(void) {
  if (reflektor_lifecycle_output != (uintptr_t*)0) {
    reflektor_lifecycle_output[0] = REFLEKTOR_DESTRUCTOR_SENTINEL;
    reflektor_lifecycle_output[1] += (uintptr_t)1u;
  }
}

REFLEKTOR_EXPORT uintptr_t ReflektorLifecycleState(void) {
  return reflektor_lifecycle_state;
}

REFLEKTOR_EXPORT uintptr_t ReflektorLifecycleBind(uintptr_t output) {
  reflektor_lifecycle_output = (uintptr_t*)output;
  if (reflektor_lifecycle_output != (uintptr_t*)0) {
    reflektor_lifecycle_output[0] = reflektor_lifecycle_state;
  }
  return reflektor_lifecycle_state;
}
