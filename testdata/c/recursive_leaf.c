#if defined(_WIN32)
#include <windows.h>
#define REFLEKTOR_EXPORT __declspec(dllexport)
#else
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#endif

static int leaf_state = 0;

#if defined(_WIN32)
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    leaf_state = 40;
  }
  return TRUE;
}
#else
__attribute__((constructor)) static void initialize_leaf(void) {
  leaf_state = 40;
}
#endif

REFLEKTOR_EXPORT int ReflektorLeafValue(void) {
  return leaf_state;
}
