#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#if defined(_WIN32)
#include <windows.h>
#define REFLEKTOR_EXPORT __declspec(dllexport)
#else
#define REFLEKTOR_EXPORT __attribute__((visibility("default")))
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
static void write_marker(const char* path) {
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
  const char payload[] = "ok";
  DWORD written = 0;
  (void)WriteFile(h, payload, 2, &written, NULL);
  CloseHandle(h);
}
#else
static void write_marker(const char* path) {
  FILE* f = fopen(path, "wb");
  if (f == NULL) {
    return;
  }
  (void)fwrite("ok", 1, 2, f);
  fclose(f);
}
#endif

REFLEKTOR_EXPORT void StartW(void) {
  write_marker(marker_path());
}

REFLEKTOR_EXPORT int StartWStatus(void) {
  StartW();
  return 1337;
}
