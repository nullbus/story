# Story

Tistory blog post manager

## Prerequisites

- Tistory OAuth credentials: 

    client id, redirect URI

    Go http://www.tistory.com/guide/api/manage/register to create credentials.

## Install

1. With Go

        go get github.com/nullbus/story/story

1. Without Go

    TODO

You must run `story init` to setup your tistory account. It configures your environment and retrives your first access token. If the access token expires, just run `story auth` to begin reauthentication.

## Usage

### Command
```
story <command> [options...]
  story init
  story auth
  story info
  story show
  story edit
  story post
```

### Get your blog information

    story info -blog <blog name>

### Get single blog post

    story show -blog <blog name> <post id>
