/* OpenBSD/amd64 executable-memory feasibility probe. */
#include <sys/mman.h>
#include <sys/wait.h>

#include <dlfcn.h>
#include <errno.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int execute_transition(const char *promises)
{
	static const unsigned char code[] = {0xb8, 42, 0, 0, 0, 0xc3};
	void *region = mmap(NULL, 4096, PROT_READ | PROT_WRITE,
	    MAP_PRIVATE | MAP_ANON, -1, 0);
	if (region == MAP_FAILED) {
		perror("mmap RW");
		return 1;
	}
	memcpy(region, code, sizeof(code));
	if (promises != NULL && pledge(promises, NULL) == -1) {
		perror("pledge");
		return 1;
	}
	if (mprotect(region, 4096, PROT_READ | PROT_EXEC) == -1) {
		perror("mprotect RW to RX");
		return 1;
	}
	int (*fn)(void) = region;
	if (fn() != 42) {
		fprintf(stderr, "mapped code returned wrong value\n");
		return 1;
	}
	return munmap(region, 4096) == 0 ? 0 : 1;
}

static int verify_child(int kind)
{
	pid_t child = fork();
	if (child == -1) {
		perror("fork");
		return 1;
	}
	if (child == 0) {
		if (kind == 0) {
			void *region = mmap(NULL, 4096,
			    PROT_READ | PROT_WRITE | PROT_EXEC,
			    MAP_PRIVATE | MAP_ANON, -1, 0);
			_exit(region == MAP_FAILED && errno == ENOTSUP ? 0 : 1);
		}
		if (kind == 1)
			_exit(execute_transition("stdio") == 0 ? 1 : 0);
		_exit(execute_transition("stdio prot_exec") == 0 ? 0 : 1);
	}
	int status;
	if (waitpid(child, &status, 0) == -1) {
		perror("waitpid");
		return 1;
	}
	/* kern.wxabort and pledge may kill the child with SIGABRT. */
	if (WIFSIGNALED(status))
		return kind != 2 && WTERMSIG(status) == SIGABRT ? 0 : 1;
	if (WIFEXITED(status) && WEXITSTATUS(status) == 0)
		return 0;
	return 1;
}

int main(void)
{
	void *self = dlopen(NULL, RTLD_NOW);
	if (self == NULL || dlsym(self, "puts") == NULL) {
		fprintf(stderr, "runtime dlsym lookup failed: %s\n", dlerror());
		return 1;
	}
	if (dlclose(self) != 0) {
		fprintf(stderr, "dlclose failed: %s\n", dlerror());
		return 1;
	}
	fputs("check unpledged RW to RX\n", stderr);
	if (execute_transition(NULL) != 0) {
		fprintf(stderr, "unpledged RW to RX transition failed\n");
		return 1;
	}
	fputs("check prot_exec pledged RW to RX\n", stderr);
	if (verify_child(2) != 0) {
		fprintf(stderr, "prot_exec pledged RW to RX transition failed\n");
		return 1;
	}
	fputs("check RWX denial\n", stderr);
	if (verify_child(0) != 0) {
		fprintf(stderr, "RWX mapping unexpectedly succeeded\n");
		return 1;
	}
	fputs("check pledge without prot_exec\n", stderr);
	if (verify_child(1) != 0) {
		fprintf(stderr, "RX transition unexpectedly succeeded without prot_exec pledge\n");
		return 1;
	}
	puts("openbsd/amd64: dlsym works, RW->RX works, RWX is denied, pledge requires prot_exec");
	return 0;
}
