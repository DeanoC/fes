#pragma once
#include <stddef.h>
void *fes_jent_alloc(size_t);
void fes_jent_free(void *);
#define malloc fes_jent_alloc
#define free fes_jent_free
