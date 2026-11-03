// Package host runs the server as a launchd job, so it starts at login and outlives the app.
package host

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const Label = "com.flarehq.polymux.server"

// passed are the settings a throwaway instance keeps out of the real server's. Nothing secret
// belongs here: a launchd job's file is readable and outlives the process.
var passed = []string{"POLYMUX_DATA_DIR", "POLYMUX_DEV", "POLYMUX_HOST_DIR", "POLYMUX_HOST_LABEL", "POLYMUX_LISTEN", "POLYMUX_LOG_DIR", "POLYMUX_NO_MDNS", "POLYMUX_TUNNEL"}

// Service is the launchd job. POLYMUX_HOST_LABEL and POLYMUX_HOST_DIR point a test instance at
// its own job, so nothing it does can touch the real one.
type Service struct {
	Label  string
	Path   string
	Log    string
	domain string
}

func Open() (Service, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Service{}, err
	}
	label := os.Getenv("POLYMUX_HOST_LABEL")
	if label == "" {
		label = Label
	}
	dir := os.Getenv("POLYMUX_HOST_DIR")
	if dir == "" {
		dir = filepath.Join(home, "Library", "LaunchAgents")
	}
	logs := os.Getenv("POLYMUX_LOG_DIR")
	if logs == "" {
		logs = filepath.Join(home, ".polymux", "logs")
	}
	return Service{Label: label, Path: filepath.Join(dir, label+".plist"), Log: filepath.Join(logs, "launchd.log"),
		domain: "gui/" + strconv.Itoa(os.Getuid())}, nil
}

func (s Service) Installed() bool {
	_, err := os.Stat(s.Path)
	return err == nil
}

// Binary is the program the installed job runs.
func (s Service) Binary() (string, error) {
	output, err := exec.Command("/usr/bin/plutil", "-extract", "ProgramArguments.0", "raw", "-o", "-", s.Path).Output()
	return strings.TrimSpace(string(output)), err
}

// Install writes the job and starts it, replacing any job already loaded.
func (s Service) Install(binary string) error {
	unit, err := Unit(s.Label, binary, s.Log, environment())
	if err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(s.Path), filepath.Dir(s.Log)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := s.stop(); err != nil {
		return err
	}
	if err := os.WriteFile(s.Path, unit, 0o600); err != nil {
		return err
	}
	return s.start()
}

// Remove stops the job and deletes it. The server's data stays.
func (s Service) Remove() error {
	if err := s.stop(); err != nil {
		return err
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Restart replaces the running process with whatever the job's binary now is.
func (s Service) Restart() error {
	return exec.Command("/bin/launchctl", "kickstart", "-k", s.domain+"/"+s.Label).Run()
}

func (s Service) loaded() bool {
	return exec.Command("/bin/launchctl", "print", s.domain+"/"+s.Label).Run() == nil
}

func (s Service) stop() error {
	if !s.loaded() {
		return nil
	}
	// A job can report an error while stopping, so only its absence counts.
	_ = exec.Command("/bin/launchctl", "bootout", s.domain+"/"+s.Label).Run()
	for range 50 {
		if !s.loaded() {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("the server didn't stop")
}

func (s Service) start() error {
	var failure []byte
	// launchd can still be releasing the job's old slot just after a stop.
	for range 5 {
		output, err := exec.Command("/bin/launchctl", "bootstrap", s.domain, s.Path).CombinedOutput()
		if err == nil {
			return nil
		}
		failure = output
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the server didn't start: %s", strings.TrimSpace(string(failure)))
}

func environment() map[string]string {
	values := map[string]string{}
	for _, name := range passed {
		if value := os.Getenv(name); value != "" {
			values[name] = value
		}
	}
	return values
}

// Unit is the launchd job: the server, kept running, with no shell in between.
func Unit(label, binary, log string, env map[string]string) ([]byte, error) {
	for _, path := range []string{binary, log} {
		if !filepath.IsAbs(path) {
			return nil, errors.New("service paths must be absolute")
		}
	}
	value := func(tag, text string) (string, error) {
		var escaped strings.Builder
		if err := xml.EscapeText(&escaped, []byte(text)); err != nil {
			return "", err
		}
		return "<" + tag + ">" + escaped.String() + "</" + tag + ">", nil
	}
	var body strings.Builder
	write := func(key, tag, text string) error {
		item, err := value(tag, text)
		if err == nil {
			body.WriteString("<key>" + key + "</key>" + item)
		}
		return err
	}
	if err := write("Label", "string", label); err != nil {
		return nil, err
	}
	body.WriteString("<key>ProgramArguments</key><array>")
	for _, argument := range []string{binary, "serve"} {
		item, err := value("string", argument)
		if err != nil {
			return nil, err
		}
		body.WriteString(item)
	}
	body.WriteString("</array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>10</integer>")
	for _, key := range []string{"StandardOutPath", "StandardErrorPath"} {
		if err := write(key, "string", log); err != nil {
			return nil, err
		}
	}
	if len(env) > 0 {
		names := make([]string, 0, len(env))
		for name := range env {
			names = append(names, name)
		}
		slices.Sort(names)
		body.WriteString("<key>EnvironmentVariables</key><dict>")
		for _, name := range names {
			if err := write(name, "string", env[name]); err != nil {
				return nil, err
			}
		}
		body.WriteString("</dict>")
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict>` + body.String() + `</dict></plist>`), nil
}
