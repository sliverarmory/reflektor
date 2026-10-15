#if !defined(_WIN32)
#error Windows delay import fixture
#endif

#define DLL_PROCESS_ATTACH 1
#define DLL_PROCESS_DETACH 0
#define EXPORT __declspec(dllexport)

static int leaf_state;
static volatile int *observer;

int __stdcall DllMain(void *instance, unsigned long reason, void *reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    leaf_state = 40;
  } else if (reason == DLL_PROCESS_DETACH) {
    if (observer != 0) {
      observer[1] = ++observer[2];
    }
    leaf_state = 0;
  }
  return 1;
}

EXPORT int ReflektorDelayValue(void) { return leaf_state + 1; }
EXPORT void ReflektorDelaySetObserver(volatile int *record) { observer = record; }
