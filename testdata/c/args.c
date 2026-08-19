#include <stdint.h>

#if defined(_WIN32)
#define REFLEKTOR_EXPORT __declspec(dllexport)
#elif defined(__GNUC__)
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#else
#define REFLEKTOR_EXPORT
#endif

#if defined(_WIN32) && defined(__i386__)
#define REFLEKTOR_CALLBACK_CALL __attribute__((stdcall))
#else
#define REFLEKTOR_CALLBACK_CALL
#endif

typedef uintptr_t (REFLEKTOR_CALLBACK_CALL *reflektor_args_callback)(
    uintptr_t sum,
    uintptr_t state);
typedef int32_t (REFLEKTOR_CALLBACK_CALL *reflektor_echo_callback)(
    char* data,
    int32_t size);

static uintptr_t reflektor_args_state;

REFLEKTOR_EXPORT void ReflektorArgsInit(void) {
  reflektor_args_state = 40;
}

REFLEKTOR_EXPORT uintptr_t REFLEKTOR_CALLBACK_CALL ReflektorArgsCallback(
    uintptr_t sum,
    uintptr_t state) {
  return (sum << 16) | (state & 0xffffu);
}

REFLEKTOR_EXPORT uintptr_t ReflektorArgsCallbackAddress(void) {
  return (uintptr_t)&ReflektorArgsCallback;
}

REFLEKTOR_EXPORT uintptr_t ReflektorArgsRun(
    uintptr_t input,
    uintptr_t input_size,
    uintptr_t callback) {
  const unsigned char* data = (const unsigned char*)input;
  uint32_t length = (uint32_t)input_size;
  uintptr_t sum = 0;
  for (uint32_t i = 0; i < length; i++) {
    sum += data[i];
  }

  reflektor_args_state++;
  if (callback == 0) {
    return 0;
  }
  return ((reflektor_args_callback)callback)(sum, reflektor_args_state);
}

REFLEKTOR_EXPORT uintptr_t ReflektorArgsState(void) {
  return reflektor_args_state;
}

REFLEKTOR_EXPORT int32_t ReflektorArgsEcho(
    uintptr_t input,
    uintptr_t input_size,
    uintptr_t callback) {
  if (callback == 0) {
    return -1;
  }
  return ((reflektor_echo_callback)callback)(
      (char*)input,
      (int32_t)(uint32_t)input_size);
}
