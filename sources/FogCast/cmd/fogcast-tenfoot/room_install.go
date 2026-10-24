package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

func roomCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("rooms", "", "local launcher's room directory")
	var output string
	if args[0] == "room-export" {
		flags.StringVar(&output, "output", "", "portable room-pack output file")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		if args[0] == "room-export" {
			fmt.Fprintln(stderr, "usage: fogcast-tenfoot room-export --output FILE PACK_DIRECTORY")
		} else if args[0] == "room-rollback" {
			fmt.Fprintln(stderr, "usage: fogcast-tenfoot room-rollback [--rooms DIRECTORY] ROOM_ID")
		} else {
			fmt.Fprintln(stderr, "usage: fogcast-tenfoot room-install [--rooms DIRECTORY] PACK_DIRECTORY_OR_ARCHIVE")
		}
		return 2
	}
	if args[0] == "room-export" {
		if output == "" {
			fmt.Fprintln(stderr, "room-export requires --output FILE")
			return 2
		}
		if err := rooms.Export(flags.Arg(0), output); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Exported room pack to %s.\n", output)
		return 0
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
