# Contributors

This is a program which analyses contributions to a Git repository and displays a report on a HTTP server.

## Prerequisites

You need to install [linguist](https://github.com/github/linguist) on your system.

## Installation

It is a Go program, so you can build and install the program using `go`.

Alternatively, if you have Nix 2.4 or later installed, you can run the program without installation:

```sh
nix run github:akirak/contributors
```

## Usage

```sh
contributors [DIR]
```

or if you don't install the executable:

```sh
go run . [DIR]
```

or with Nix:

```sh
nix run github:akirak/contributors -- [DIR]
```
