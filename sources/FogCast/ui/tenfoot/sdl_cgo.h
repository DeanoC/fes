#ifndef FOGCAST_TENFOOT_SDL_CGO_H
#define FOGCAST_TENFOOT_SDL_CGO_H

#include <SDL3/SDL.h>

enum {
	FC_EV_NONE = 0,
	FC_EV_QUIT,
	FC_EV_KEY,
	FC_EV_BUTTON,
	FC_EV_AXIS,
	FC_EV_PAD_ADDED,
	FC_EV_PAD_REMOVED,
	FC_EV_TEXT,
	FC_EV_MOUSE_MOVE,
	FC_EV_MOUSE_BUTTON,
	FC_EV_KEY_ADDED,
	FC_EV_KEY_REMOVED,
	FC_EV_MOUSE_ADDED,
	FC_EV_MOUSE_REMOVED,
	FC_EV_FOCUS_LOST
};

typedef struct FogcastEvent {
	int kind;
	int code;
	int value;
	int which;
	int down;
	int x;
	int y;
	double dx;
	double dy;
	char *text;
} FogcastEvent;

extern void fogcast_update_pads(void);
extern int fogcast_poll(FogcastEvent *out, SDL_Renderer *renderer);
extern SDL_JoystickID fogcast_attach_virtual_gamepad(void);
extern int fogcast_virtual_button_on(SDL_Gamepad *pad, int button, int down);

#endif
