//go:build !windows

package main

import "context"

type App struct{}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(context.Context) {}

func (a *App) domReady(context.Context) {}

func (a *App) shutdown(context.Context) {}

func (a *App) beforeClose(context.Context) bool { return false }

func (a *App) startHidden() bool { return false }
