#include <windows.h>

#define EXPORT __declspec(dllexport)
#define IMPORT __declspec(dllimport)

static void log_event(char event) {
  char path[MAX_PATH];
  DWORD length = GetEnvironmentVariableA("REFLEKTOR_GRAPH_LOG", path, MAX_PATH);
  if (length == 0 || length >= MAX_PATH) {
    return;
  }
  HANDLE file = CreateFileA(path, FILE_APPEND_DATA, FILE_SHARE_READ | FILE_SHARE_WRITE,
                            NULL, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
  if (file == INVALID_HANDLE_VALUE) {
    return;
  }
  DWORD written;
  (void)WriteFile(file, &event, 1, &written, NULL);
  (void)CloseHandle(file);
}

#if defined(REFLEKTOR_CYCLE_A)

#if !defined(REFLEKTOR_GRAPH_BOOTSTRAP)
IMPORT int CycleBValue(void);
IMPORT int CycleBCallA(void);
IMPORT int CycleForwardedA(void);
#endif

static int state;
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
#if !defined(REFLEKTOR_GRAPH_BOOTSTRAP)
    state = CycleBValue() == 22 ? 20 : -100;
#endif
    log_event('A');
  } else if (reason == DLL_PROCESS_DETACH) {
#if !defined(REFLEKTOR_GRAPH_BOOTSTRAP)
    if (CycleBValue() != 22) log_event('!');
#endif
    log_event('a');
  }
  return TRUE;
}
EXPORT int CycleAValue(void) {
#if defined(REFLEKTOR_GRAPH_BOOTSTRAP)
  return 0;
#else
  return state + CycleBValue();
#endif
}
EXPORT int CycleRoundTrip(void) {
#if defined(REFLEKTOR_GRAPH_BOOTSTRAP)
  return 0;
#else
  return CycleBCallA();
#endif
}
EXPORT int CycleForwardedResult(void) {
#if defined(REFLEKTOR_GRAPH_BOOTSTRAP)
  return 0;
#else
  return CycleForwardedA();
#endif
}

#elif defined(REFLEKTOR_CYCLE_B)

IMPORT int CycleAValue(void);
static int state;
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    state = 22;
    log_event('B');
  } else if (reason == DLL_PROCESS_DETACH) {
    if (CycleAValue() != 42) log_event('!');
    log_event('b');
  }
  return TRUE;
}
EXPORT int CycleBValue(void) { return state; }
EXPORT int CycleBCallA(void) { return CycleAValue() + 1; }

#elif defined(REFLEKTOR_DIAMOND_LEAF)

static int state;
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    state = 10;
    log_event('L');
  } else if (reason == DLL_PROCESS_DETACH) {
    log_event('l');
  }
  return TRUE;
}
EXPORT int DiamondLeafValue(void) { return state; }

#elif defined(REFLEKTOR_DIAMOND_LEFT)

IMPORT int DiamondLeafValue(void);
static int state;
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    state = DiamondLeafValue() + 1;
    log_event('X');
  } else if (reason == DLL_PROCESS_DETACH) {
    log_event('x');
  }
  return TRUE;
}
EXPORT int DiamondLeftValue(void) { return state; }

#elif defined(REFLEKTOR_DIAMOND_RIGHT)

IMPORT int DiamondLeafValue(void);
static int state;
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    state = DiamondLeafValue() + 2;
    log_event('Y');
  } else if (reason == DLL_PROCESS_DETACH) {
    log_event('y');
  }
  return TRUE;
}
EXPORT int DiamondRightValue(void) { return state; }

#elif defined(REFLEKTOR_DIAMOND_ROOT)

IMPORT int DiamondLeftValue(void);
IMPORT int DiamondRightValue(void);
static int state;
BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    state = DiamondLeftValue() + DiamondRightValue() + 19;
    log_event('R');
  } else if (reason == DLL_PROCESS_DETACH) {
    log_event('r');
  }
  return TRUE;
}
EXPORT int DiamondResult(void) { return state; }

#else
#error Select a recursive graph fixture variant.
#endif
