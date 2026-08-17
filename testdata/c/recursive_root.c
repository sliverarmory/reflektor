#include <stdio.h>
#include <stdlib.h>

#if defined(_WIN32)
#include <windows.h>
#define REFLEKTOR_EXPORT __declspec(dllexport)
#define REFLEKTOR_IMPORT __declspec(dllimport)
#else
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
#define REFLEKTOR_IMPORT
#endif

REFLEKTOR_IMPORT int ReflektorMiddleValue(void);

static int root_state = 0;

#if defined(_WIN32)
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    root_state = ReflektorMiddleValue() + 1;
  }
  return TRUE;
}
#else
__attribute__((constructor)) static void initialize_root(void) {
  root_state = ReflektorMiddleValue() + 1;
}
#endif

static const char* marker_path(void) {
  const char* env = getenv("REFLEKTOR_MARKER");
  if (env != NULL && env[0] != '\0') {
    return env;
  }
#if defined(_WIN32)
  return "C:\\Windows\\Temp\\reflektor_marker.txt";
#else
  return "/tmp/reflektor_marker.txt";
#endif
}

#if defined(_WIN32)
static void write_marker(const char* path, const char* payload) {
  HANDLE h = CreateFileA(
      path,
      GENERIC_WRITE,
      FILE_SHARE_READ | FILE_SHARE_WRITE,
      NULL,
      CREATE_ALWAYS,
      FILE_ATTRIBUTE_NORMAL,
      NULL);
  if (h == INVALID_HANDLE_VALUE) {
    return;
  }
  DWORD written = 0;
  (void)WriteFile(h, payload, 2, &written, NULL);
  CloseHandle(h);
}
#else
static void write_marker(const char* path, const char* payload) {
  FILE* f = fopen(path, "wb");
  if (f == NULL) {
    return;
  }
  (void)fwrite(payload, 1, 2, f);
  fclose(f);
}
#endif

REFLEKTOR_EXPORT void StartW(void) {
  const char* payload = "no";
  if (root_state == 42 && ReflektorMiddleValue() == 41) {
    payload = "ok";
  }
  write_marker(marker_path(), payload);
}
