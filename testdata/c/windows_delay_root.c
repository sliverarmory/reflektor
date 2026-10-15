#if !defined(_WIN32)
#error Windows delay import fixture
#endif

#define DLL_PROCESS_ATTACH 1
#define DLL_PROCESS_DETACH 0
#define EXPORT __declspec(dllexport)
#define IMPORT __declspec(dllimport)

IMPORT int ReflektorDelayValue(void);
IMPORT void ReflektorDelaySetObserver(volatile int *record);

static int root_state;
static volatile int *observer;

// The recursive loader eagerly fills the delay IAT before DllMain. This
// fallback is needed to satisfy the linker but must never be called.
void *__stdcall __delayLoadHelper2(void *descriptor, void **slot) {
  (void)descriptor;
  (void)slot;
  return 0;
}

int __stdcall DllMain(void *instance, unsigned long reason, void *reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    root_state = ReflektorDelayValue() + 1;
  } else if (reason == DLL_PROCESS_DETACH) {
    if (observer != 0) {
      observer[0] = ++observer[2];
    }
    root_state = 0;
  }
  return 1;
}

EXPORT int DelayValue(void) { return root_state; }
EXPORT void DelaySetObserver(volatile int *record) {
  observer = record;
  ReflektorDelaySetObserver(record);
}
