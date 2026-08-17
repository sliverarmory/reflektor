#if defined(_WIN32)
#include <windows.h>
#define REFLEKTOR_EXPORT __declspec(dllexport)
#define REFLEKTOR_IMPORT __declspec(dllimport)
#else
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#define REFLEKTOR_IMPORT
#endif

REFLEKTOR_IMPORT int ReflektorLeafValue(void);

static int middle_state = 0;

#if defined(_WIN32)
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    middle_state = ReflektorLeafValue() + 1;
  }
  return TRUE;
}
#else
__attribute__((constructor)) static void initialize_middle(void) {
  middle_state = ReflektorLeafValue() + 1;
}
#endif

REFLEKTOR_EXPORT int ReflektorMiddleValue(void) {
  return middle_state;
}
