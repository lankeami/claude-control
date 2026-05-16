# Getting Started with Claude Controller

Claude Controller lets you control Claude Code sessions from any browser — including your phone. This guide walks you through getting it running on your computer in just a few steps.

---

## Prerequisites

You need two things before you start:

1. **Claude Code installed** — Claude Controller works alongside Claude Code. If you don't have it yet, [download Claude Code here](https://claude.ai/download).

2. **Go installed** — Go is the programming language Claude Controller is built with. If you don't have it:
   - **Mac:** Open Terminal and run `brew install go`
     - (If you don't have Homebrew, install it first at [brew.sh](https://brew.sh))
   - **Windows:** Download and run the installer from [go.dev/dl](https://go.dev/dl/)
   - After installing, close and reopen your terminal before continuing.

That's it. You do not need to know how to code.

---

## Quickstart (Mac and Linux)

### Step 1 — Download Claude Controller

Open **Terminal** (on Mac, press `Cmd + Space`, type "Terminal", hit Enter).

Paste this command and press Enter:

```bash
git clone https://github.com/jaychinthrajah/claude-controller.git
cd claude-controller
```

> **No git?** On Mac, running the command above will prompt you to install it automatically. On Windows, download [Git for Windows](https://git-scm.com/download/win) first.

### Step 2 — Run the quickstart command

Still in the same Terminal window, paste this and press Enter:

```bash
make quickstart
```

That's it. This single command will:

- Check that all required tools are installed
- Set up your configuration file
- Download all dependencies
- Build and start the server on port 9999
- Open Claude Controller in your browser automatically

The first run takes about 30–60 seconds while it downloads and builds everything. Subsequent runs are much faster.

### Step 3 — Install the hooks

In a new Terminal tab (press `Cmd + T`), paste this and press Enter:

```bash
make hooks
```

This connects Claude Code to Claude Controller so your sessions appear in the web UI. After running it, restart any Claude Code windows you have open.

### Step 4 — You're done

Claude Controller is now running at **http://localhost:9999**. Open that address in any browser.

To stop the server, press `Ctrl + C` in the Terminal window where it's running.

To start it again next time, just run:

```bash
make quickstart
```

---

## Quickstart (Windows)

### Step 1 — Download Claude Controller

Open **PowerShell** (press `Win + X`, choose "Windows PowerShell" or "Terminal").

Paste this and press Enter:

```powershell
git clone https://github.com/jaychinthrajah/claude-controller.git
cd claude-controller
```

### Step 2 — Run the quickstart command

```powershell
make quickstart
```

> **Note:** If `make` is not recognized, install it via `winget install GnuWin32.Make` and reopen PowerShell.

### Step 3 — Install the hooks

In a new PowerShell window:

```powershell
make hooks
```

Restart any Claude Code windows after this step.

### Step 4 — You're done

Claude Controller is running at **http://localhost:9999**. Open it in any browser.

---

## Troubleshooting

**"command not found: go"**
Go is not installed or not on your PATH. Quit and reopen your terminal after installing Go, then try again.

**"Port 9999 already in use"**
Another program is using that port. Stop it, or run `make stop` then `make quickstart` again.

**The browser opened but shows an error**
The server may still be starting. Wait 5 seconds and refresh the page.

**Claude sessions aren't showing up**
Make sure you ran `make hooks` and restarted Claude Code afterward.

---

## Setting Up ngrok (Remote Access)

By default, Claude Controller only runs on your local computer. If you want to access it from your phone, another computer, or from anywhere in the world, you can use **ngrok** to create a secure public link.

### What is ngrok?

ngrok is a free tool that creates a temporary public web address for your computer. When you share that address with your phone, it connects securely back to Claude Controller running on your Mac or PC — no complicated network setup required.

### Step 1 — Create a free ngrok account

Go to [ngrok.com](https://ngrok.com) and sign up for a free account.

### Step 2 — Download and install ngrok

After signing up, ngrok will show you a download page with instructions for your operating system. Follow those steps.

- **Mac (recommended):** `brew install ngrok/ngrok/ngrok`
- **Windows:** Download the installer from [ngrok.com/download](https://ngrok.com/download) and run it.

### Step 3 — Connect your account

In your ngrok dashboard, find your **authtoken** — it's a long string of letters and numbers on the "Your Authtoken" page.

Run this command, replacing `YOUR_TOKEN` with your actual token:

```bash
ngrok config add-authtoken YOUR_TOKEN
```

You only need to do this once.

### Step 4 — Start ngrok

With Claude Controller already running (`make quickstart`), open a second Terminal window and run:

```bash
make ngrok
```

ngrok will display a public URL that looks something like:

```
Forwarding   https://abc123.ngrok-free.app -> http://localhost:9999
```

### Step 5 — Access Claude Controller remotely

Open that `https://abc123.ngrok-free.app` URL on your phone or any other device. It connects directly to your Claude Controller.

> **Security note:** Anyone with this URL can access your Claude Controller. Keep it private. ngrok free accounts generate a new URL each time you start it. Stop ngrok (Ctrl+C) when you're not using remote access.

### Tip: Using the iOS app

If you have the Claude Controller iOS app, scan the QR code that appears in the Terminal when Claude Controller starts. The QR code contains everything the app needs to connect — including the ngrok URL.

---

## What's Next?

- **Hook mode** — Claude Code sessions you run in your terminal appear in the web UI automatically (after `make hooks`)
- **Managed mode** — Start new Claude sessions directly from the browser, with full control over tools, turn limits, and more
- **iOS app** — Pair the iPhone app to manage sessions from anywhere
