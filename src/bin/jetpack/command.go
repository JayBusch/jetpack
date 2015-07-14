package main

import (
	"crypto/sha512"
	stderrors "errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"unicode"

	"code.google.com/p/go-uuid/uuid"

	"github.com/appc/spec/discovery"
	"github.com/appc/spec/schema/types"
	"github.com/juju/errors"

	"lib/jetpack"
)

var ErrUsage = stderrors.New("Invalid usage")

type CommandHandler func([]string) error

type Command struct {
	Usage, Synopsis string
	handler         CommandHandler
	flag            *flag.FlagSet
}

var Commands = make(map[string]*Command)

func AddCommand(usage, synopsis string, handler CommandHandler, flHandler func(*flag.FlagSet)) *Command {
	space := strings.IndexFunc(usage, unicode.IsSpace)
	if space < 0 {
		space = len(usage)
	}
	name := usage[:space]

	cmd := &Command{
		Usage:    usage,
		Synopsis: synopsis,
		handler:  handler,
	}

	if flHandler != nil {
		cmd.flag = flag.NewFlagSet(name, flag.ExitOnError)
		flHandler(cmd.flag)
	}

	Commands[name] = cmd
	return cmd
}

func (cmd *Command) String() string {
	if cmd.Synopsis != "" {
		return fmt.Sprintf("%v -- %v", cmd.Usage, cmd.Synopsis)
	}
	return cmd.Usage
}

func (cmd *Command) Help() {
	fmt.Fprintf(os.Stderr, "%v\n\nUsage: %v %v\n",
		cmd.Synopsis, AppName, cmd.Usage)
	if cmd.flag != nil {
		fmt.Fprintln(os.Stderr, "Options:")
		cmd.flag.PrintDefaults()
	}
}

func (cmd *Command) Run(args []string) error {
	if cmd.flag != nil {
		cmd.flag.Parse(args)
		args = cmd.flag.Args()
	}
	err := cmd.handler(args)
	if err == ErrUsage {
		return fmt.Errorf("Usage: %v %v", AppName, cmd.Usage)
	}
	return err
}

func cmdWrap(fn func()) CommandHandler {
	return func([]string) error { fn(); return nil }
}

func cmdWrapErr(fn func() error) CommandHandler {
	return func([]string) error { return errors.Trace(fn()) }
}

func cmdWrapImage(cmd func(*jetpack.Image, []string) error, localOnly bool) func([]string) error {
	return func(args []string) error {
		if len(args) == 0 {
			return ErrUsage
		}
		if img, err := getImage(args[0], localOnly); err != nil {
			return errors.Trace(err)
		} else {
			return errors.Trace(cmd(img, args[1:]))
		}
	}
}

func cmdWrapImage0(cmd func(*jetpack.Image) error, localOnly bool) func([]string) error {
	return cmdWrapImage(func(img *jetpack.Image, args []string) error {
		if len(args) > 0 {
			return ErrUsage
		}
		return cmd(img)
	}, localOnly)
}

func cmdWrapPod(cmd func(*jetpack.Pod, []string) error) func([]string) error {
	return func(args []string) error {
		if len(args) == 0 {
			return ErrUsage
		}
		if pod, err := getPod(args[0]); err != nil {
			return errors.Trace(err)
		} else {
			return errors.Trace(cmd(pod, args[1:]))
		}
	}
}

func cmdWrapPod0(cmd func(*jetpack.Pod) error) func([]string) error {
	return cmdWrapPod(func(pod *jetpack.Pod, args []string) error {
		if len(args) > 0 {
			return ErrUsage
		}
		return cmd(pod)
	})
}

func cmdWrapApp(cmd func(*jetpack.Pod, types.ACName, []string) error) func([]string) error {
	return func(args []string) error {
		if len(args) == 0 {
			return ErrUsage
		}
		pieces := strings.SplitN(args[0], ":", 2)
		if pod, err := getPod(pieces[0]); err != nil {
			return errors.Trace(err)
		} else if len(pieces) == 1 {
			if len(pod.Manifest.Apps) == 1 {
				return errors.Trace(cmd(pod, pod.Manifest.Apps[0].Name, args[1:]))
			} else {
				return errors.Trace(cmd(pod, types.ACName(""), args[1:]))
			}
		} else if appName, err := types.NewACName(pieces[1]); err != nil {
			return errors.Trace(err)
		} else if rta := pod.Manifest.Apps.Get(*appName); rta == nil {
			return errors.Errorf("Pod %v has no app %v", pod.UUID, appName)
		} else {
			return errors.Trace(cmd(pod, *appName, args[1:]))
		}
	}
}

func cmdWrapApp0(cmd func(*jetpack.Pod, types.ACName) error) func([]string) error {
	return cmdWrapApp(func(pod *jetpack.Pod, appName types.ACName, args []string) error {
		if len(args) > 0 {
			return ErrUsage
		}
		return cmd(pod, appName)
	})
}

func cmdWrapMustApp(cmd func(*jetpack.Pod, types.ACName, []string) error) func([]string) error {
	return cmdWrapApp(func(pod *jetpack.Pod, appName types.ACName, args []string) error {
		if appName.Empty() {
			return errors.Errorf("No app name provided, and pod %v has multiple apps", pod.UUID)
		}
		return cmd(pod, appName, args)
	})
}

func cmdWrapMustApp0(cmd func(*jetpack.Pod, types.ACName) error) func([]string) error {
	return cmdWrapMustApp(func(pod *jetpack.Pod, appName types.ACName, args []string) error {
		if len(args) > 0 {
			return ErrUsage
		}
		return cmd(pod, appName)
	})
}

func parseImageName(name string) (types.ACIdentifier, types.Labels, error) {
	app, err := discovery.NewAppFromString(name)
	if err != nil {
		return "", nil, errors.Trace(err)
	}

	if app.Labels["os"] == "" {
		app.Labels["os"] = runtime.GOOS
	}
	if app.Labels["arch"] == "" {
		app.Labels["arch"] = runtime.GOARCH
	}

	labels, err := types.LabelsFromMap(app.Labels)
	if err != nil {
		return "", nil, errors.Trace(err)
	}

	return app.Name, labels, nil
}

const hashSize = sha512.Size*2 + len("sha512-")

func getImage(name string, localOnly bool) (*jetpack.Image, error) {
	if h, err := types.NewHash(name); err == nil {
		if len(name) < hashSize {
			// Short hash. Iterate over images, return first prefix match.
			// FIXME: what about multiple matches?
			name = strings.ToLower(name)
			if imgs, err := Host.Images(); err != nil {
				return nil, errors.Trace(err)
			} else {
				for _, img := range imgs {
					if strings.HasPrefix(img.Hash.String(), name) {
						return img, nil
					}
				}
				return nil, jetpack.ErrNotFound
			}
		}
		return Host.GetImage(*h, "", nil)
	}
	if name, labels, err := parseImageName(name); err != nil {
		return nil, errors.Trace(err)
	} else if localOnly {
		return Host.GetLocalImage(types.Hash{}, name, labels)
	} else {
		return Host.GetImage(types.Hash{}, name, labels)
	}
}

func getPod(name string) (*jetpack.Pod, error) {
	if id := uuid.Parse(name); id != nil {
		// Pod UUID
		return Host.GetPod(id)
	}
	// TODO: pod name
	return nil, ErrUsage
}
