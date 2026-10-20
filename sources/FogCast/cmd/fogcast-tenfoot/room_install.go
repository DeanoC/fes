package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

func roomCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("rooms", os.Getenv("FOGCAST_ROOMS"), "local launcher's room directory")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: fogcast-tenfoot", args[0], "[--rooms DIRECTORY] PACK_DIRECTORY_OR_ROOM_ID")
		return 2
	}
	if *root == "" {
		*root = tenfoot.RoomInstallDirectory()
	}
	if args[0] == "room-rollback" {
		if err := rooms.Rollback(*root, flags.Arg(0), ""); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "Room selection rolled back. Reopen Rooms to use the previous or default version.")
		return 0
	}
	pack, err := rooms.Install(*root, flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Installed %s (%s). Reopen Rooms and select it to load this version.\n", pack.ID, pack.Revision)
	return 0
}
