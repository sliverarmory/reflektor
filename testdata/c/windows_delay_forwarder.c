#if !defined(_WIN32)
#error Windows delay import fixture
#endif

int __stdcall DllMain(void *instance, unsigned long reason, void *reserved) {
  (void)instance;
  (void)reason;
  (void)reserved;
  return 1;
}
