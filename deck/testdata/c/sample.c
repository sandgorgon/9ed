#include <stdio.h>
#include <stdlib.h>

// Point is a 2D coordinate.
typedef struct {
	int x;
	int y;
} Point;

enum Color { RED, GREEN, BLUE };

int global_count;

int add(int a, int b) {
	return a + b;
}

int main(int argc, char **argv) {
	struct Point p = { 1, 2 };
	printf("{ %d, %d }\n", p.x, p.y);
	return 0;
}
