# 🛠️ RRC Inventory - Maintainer Runbook

For whoever looks after the server next. The lab has few maintainers and students move
on, so this is written to be followed without the person who wrote it. The
[README](../README.md) covers what the site does for users; this covers keeping it running.

---

## 📦 What runs where

Everything runs with Docker Compose on one machine (`docker-compose.yml`):

| Service | What it is |
|---|---|
| `db` | PostgreSQL 16. Not reachable from outside the machine. |
| `backend` | The Go API (`backend/`). Updates the database tables on every start. |
| `frontend` | The SvelteKit site (`frontend/`). |
| `caddy` | Reverse proxy on ports 80 and 443, in front of the other two (`Caddyfile`). |

Data lives in named volumes, which survive stops, reboots and rebuilds:

| Volume | Holds |
|---|---|
| `postgres_data` | Everything: users, loans, items, bookings, notifications, printer codes |
| `uploads_data` | Item photos |
| `caddy_data` | The HTTPS certificate authority (keep it, or every device must re-trust a new one) |
| `caddy_config` | Caddy's own state |

> ⚠️ Never run `docker compose down -v` - it deletes the volumes. `./stop.sh` is safe.

Settings live in `.env` (copied from `.env.example`, never committed): database password,
first admin login, `SITE_HOST`, `ALLOWED_ORIGINS`, `PRINTERS`, `TZ`. The site is only
reachable on the IIIT network (wifi@iiith or OpenVPN).

---

## 🚀 First install

1. Install Docker and Docker Compose. Add the user that will run the system to the
   `docker` group.
2. `git clone` the repo and `cd` into it.
3. `cp .env.example .env` and set at least `POSTGRES_PASSWORD`, `ADMIN_USERNAME`,
   `ADMIN_PASSWORD`, `SITE_HOST` and `ALLOWED_ORIGINS` (see the README).
4. `./setup.sh` (builds the images), then `./start.sh`.
5. Open `http://<server-ip>/login` and log in with `ADMIN_USERNAME` in the email field.
   If you left `ADMIN_PASSWORD` empty, the generated password is in `./logs.sh`.
6. Change that password (**Account** page), then make a second Server Admin (below).
7. Set up the nightly backup cron job (README, "Automatic daily backups").

Moving to a new machine: do steps 1-4, copy `.env` and a backup archive over, then
`./restore.sh <archive>`.

---

## ⬆️ Upgrading

```bash
./backup.sh            # always first - this is your undo
git pull
./restart.sh --build   # rebuild and start the new version
./logs.sh              # watch the backend start cleanly
```

`./restart.sh` without `--build` restarts the old images and does not pick up new code.
If the new version misbehaves, `git checkout` the previous commit, `./restart.sh --build`,
and `./restore.sh` the backup you just made.

**Upgrading from the version without accounts:** on first start the old admin logins
become accounts (they type their old username in the email field, same password):
former super admins become Server Admins, other admins go into a "Lab Admin" group
without user management, and existing mocap bookings move onto the Motion Capture Lab calendar.
Everyone else signs up fresh at `/register`.

---

## 🖱️ Day-to-day tasks

Most of this is on the **Admin** page (the Admin link appears once you log in with
management access); threads are on the **Threads** page. Users only see the tabs their
permissions allow.

### Check what needs doing
Open **📊 Dashboard**. **Needs you** lists only things you can act on (sign-ups, approvals,
thread access requests, overdue loans, reported problems, unconfirmed tracked items, fewer
than two Server Admins), each linking to where you deal with it. An empty list means
nothing is waiting. There is nothing to set up.

### Approve a sign-up
**Admin → 👥 Users → Waiting for approval**: check the group they picked (change it in the
dropdown if wrong), then **✓ Approve**. **Reject** disables the account.

### Reset a forgotten password
There is no email. **Admin → 👥 Users → Everyone → Reset password** on their row, type a
temporary password (8+ characters), tell them in person, and ask them to change it on their
**Account** page. Resetting logs them out everywhere.

### Make someone a Lab Admin

Lab Admins are the designated people who sign off on robot bookings, threads' robot access
and loans, and look after the hardware (inventory, printers, robots). **Admin → 👥 Users → Everyone**: set their
group dropdown to **Lab Admin**. Profs don't sign off; give an individual the approve
permissions under **Access** only if you really mean it.

### Make someone a Server Admin
**Admin → 👥 Users → Everyone**: set their group dropdown to **Server Admin**. You cannot
change your own account, so another admin has to change yours.

> 🔑 **Keep at least two Server Admins at all times.** If the only one leaves, nobody can
> approve sign-ups or reset passwords. Before you hand over, make your successor a Server
> Admin and check they can log in.

### Change what a group can do, or give one person extra access
- Whole group: **Admin → 👥 Users → Groups** grid, tick or untick.
- One person: **Access** on their row, then grant or revoke individual permissions.

Defaults: Student and Intern borrow, book mocap and mech, and send prints. Prof adds *own
bookings and loans need no sign-off* and *create research threads*, but does not sign off
for others. Lab Admin adds approving robot bookings and loans and managing inventory,
printers and bookings. Server Admin has everything.

### Set up a robot
**Admin → Bookings → 🦾 Robots and labs → + Add robot**:
- **Name** (e.g. "UR5 #1") and **Id** (e.g. `ur5-1`, lowercase; it cannot be changed later).
- **Lab**: Mech Lab or Mocap Lab.
- **Only research threads with access can book it** - tick this for robots booked by
  thread. Also **Every booking needs a lab admin's approval** and **Bookable**.
- **Longest booking** in hours (0 = 12) and **Upcoming bookings per person** (0 = no limit).

To retire a robot, **Edit** it and untick **Bookable**.

### Create a thread and add members
A prof (or anyone with *create research threads*): **Threads** page → create the thread
with a name and description. You are its lead. Add or remove members on the thread card,
then ask for access to the robots it needs; a lab admin gets a 🔔 notification.

### Sign off a thread's robot access, set weekly hours, or withdraw it
Lab Admins (*approve robot bookings*): **Threads** page → **Waiting for your sign-off**.
Approve with a weekly allowance in hours (0 = no limit), or decline (the request just
disappears). To change the hours or withdraw access later, use the thread's card. Withdrawing
stops new bookings; bookings already made stay. The allowance counts the thread's pending and
approved hours on that robot in the Monday-Sunday week; Lab Admins can book over it. Lab
Admins can also edit or deactivate any thread there.

### Unblock someone
A person cannot borrow or book while either is true:
- **A loan 7+ days overdue.** Best: they answer the notice on their **Account** page (still
  using / returned / lost). If they can't: **Admin →** the lab's tab → that loan →
  **📅 Extend**, or **❓ Can't Find This Item** if it is really gone.
- **Two no-shows in 30 days.** If a report was wrong, someone with *manage bookings*
  opens that booking on the calendar and clicks **Clear no-show**. Otherwise it lifts by
  itself once the older no-show is 30 days old.

### Merge duplicate items
**Admin → 🧰 Items**: on the duplicate's row pick the real item in **Merge into…**, then click
**Merge into**. Its loans move to the real item and the duplicate is deleted.

### Mark an item tracked (or lab only / needs approval)
**Admin → 🧰 Items → Edit** on its row: tick **Tracked**, pick a **Custodian**, **Save**. The
custodian then gets a monthly "is it where the system says?" check. **Lab only** and
**Borrowing needs approval** are on the same form.

### Book for someone
Anyone with *manage bookings* sees extra name/phone fields when booking, for people who
ask in person. Thread membership, per-person limits and the no-show pause do not apply to
these bookings, but a robot that only threads can book still needs a thread with access.

---

## 💾 Backups and restore

Commands and details are in the README ("Data, backups and portability"). In short:

- `./backup.sh` - one archive in `backups/` with the database and photos.
- `./backup.sh --cron` nightly via crontab, keeps 14.
- `./restore.sh backups/<archive>` - saves a safety copy first, then restores.
- Copy archives off the machine regularly. Check `backups/backup.log` is empty.

---

## 🧪 Running the tests

```bash
cd backend && go test ./...
```

No database is needed. Some printer tests start fake printers on `127.0.0.1`, so they fail
in sandboxes that forbid opening local ports - that is the environment, not the code. Run
the tests before deploying any backend change.

---

## 🗺️ Where the code lives

**Backend** (`backend/`, Go + Gin + GORM):

| File | What's in it |
|---|---|
| `main.go` | Start-up, database migration, the router (every route is registered here or from the files below), loans and admin routes |
| `accounts.go` | Users, groups, permissions, sessions (stored in the database, 14 days), sign-up and login |
| `bookings.go` | Robots/labs ("resources"), bookings, approvals, no-shows, notifications |
| `threads.go` | Research threads, members, robot access and weekly allowances |
| `dashboard.go` | The Dashboard: needs you, my threads, lab usage |
| `inventory.go` | Item catalogue, borrowing, returns, hand-over, overdue answers, hourly reminders, merge |
| `printers*.go`, `printer_*.go` | Bambu printer status, camera, file upload, start/stop |
| `*_test.go` | Tests |

**Frontend** (`frontend/src/`, SvelteKit):

| Path | What's in it |
|---|---|
| `routes/+page.svelte` | Home page: borrow and return |
| `routes/items/` | What's in the lab |
| `routes/mocap/`, `routes/mechlab/` | Booking pages (both use `lib/BookingPage.svelte`) |
| `routes/account/` | My loans, bookings, overdue answers, password |
| `routes/threads/` | Research threads |
| `routes/dashboard/` | Dashboard |
| `routes/login/`, `routes/register/` | Log in and sign up |
| `routes/admin/` | The Admin page (tabs open directly with `/admin?tab=users`, `items`, `missing`, `bookings`, `printers`, `history`) |
| `routes/printers/` | 3D printers |
| `lib/auth.js` | Login state and API calls with the session token |
| `lib/BookingPage.svelte` | The calendar shared by both labs |
| `lib/NotificationBell.svelte` | The 🔔 bell |

Permission names used in code: `borrow_items`, `book_mocap`, `book_mech`,
`approve_mech_bookings`, `approve_loans`, `auto_approve`, `create_threads`, `upload_prints`,
`manage_inventory`, `manage_printers`, `manage_bookings`, `manage_users`. The server enforces
all of them; the frontend only hides buttons.
