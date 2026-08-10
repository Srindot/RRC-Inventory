# RRC Inventory Management System

<div align="center">
  <img src="rrc_logo.png" alt="RRC Logo" width="200"/>
</div>

<div align="center">
  <h3> Simple Lab Equipment Management</h3>
  <p style="color: #f2cdcd;">A web application for managing lab equipment inventory of Robotics Research Centre</p>
</div>

<div align="center">
  
![Catppuccin](https://img.shields.io/badge/catppuccin-mocha-f2cdcd?style=for-the-badge&logo=catppuccin&logoColor=white)
![Docker](https://img.shields.io/badge/docker-ready-f2cdcd?style=for-the-badge&logo=docker&logoColor=white)
![SvelteKit](https://img.shields.io/badge/sveltekit-frontend-f2cdcd?style=for-the-badge&logo=svelte&logoColor=white)
![Go](https://img.shields.io/badge/go-backend-f2cdcd?style=for-the-badge&logo=go&logoColor=white)

</div>

---

## 📸 Preview

<div align="center">
  <img src="preview.png" alt="RRC Inventory System Preview" width="80%" style="border-radius: 12px; box-shadow: 0 8px 32px rgba(242, 205, 205, 0.3);">
</div>

---

## 🚀 Quick Start

### Prerequisites
- Docker and Docker Compose installed on your system

### Getting Started

1. **First-time setup (run once):**
   ```bash
   ./setup.sh
   ```

2. **Start the application:**
   ```bash
   ./start.sh
   ```

3. **Network access (host IP)**
  Use the server IP to access the application. mDNS/Bonjour support has been removed from this distribution; IP access is more reliable across networks and environments.

4. **Access the system:**
  - **Local Access**: http://localhost
  - **Network Access** (via IP): http://[SERVER-IP]
   - **Admin Login**: the credentials you set in `.env` (see below)

> **🔐 Admin credentials:** Copy `.env.example` to `.env` and set `POSTGRES_PASSWORD`,
> `ADMIN_USERNAME` and `ADMIN_PASSWORD` before the first start. The first admin account is
> created only once, on an empty database. If `ADMIN_PASSWORD` is left empty, a random
> password is generated and printed in the backend logs (`./logs.sh`). Never commit `.env`.

### 🎥 Motion Capture Lab booking

Open **Motion Capture Lab** from the home page (or go to `/mocap`) for a week calendar of
who has the lab. Click any empty slot to book it - no approval, the slot just has to be
free. Bookings are cancelled by whoever made them using the phone number they booked with,
and admins can delete any booking from the **Mocap Bookings** tab.

> **🌐 Network Access Note:** This website is hosted locally on a server. To access it, you need to be connected to **wifi@iiith** or use **OpenVPN** to connect to the IIIT network.

5. **Stop the application:**
   ```bash
   ./stop.sh
   ```

---

## 🛠️ Technology Stack

<table align="center">
<tr>
<td align="center"><strong>Frontend</strong></td>
<td align="center"><strong>Backend</strong></td>
<td align="center"><strong>Database</strong></td>
<td align="center"><strong>Deployment</strong></td>
</tr>
<tr>
<td align="center">SvelteKit + TypeScript</td>
<td align="center">Go + Gin Framework</td>
<td align="center">PostgreSQL</td>
<td align="center">Docker Compose</td>
</tr>
</table>

---

## ✨ Features

### 👥 For Students & Staff
- **Borrow Equipment**: Submit requests with photos and details
- **Return Items**: Mark items as returned when done
- **Track Status**: View all your borrowed items and their status

### 🛡️ For Administrators
- **Approve Requests**: Review and approve/deny borrow requests
- **Manage Returns**: Process return requests and mark items as found/missing
- **View History**: Complete searchable history of all equipment
- **Admin Management**: Add/remove administrators (Super Admin only)

---

## 📖 Usage

1. **Visit** http://[SERVER-IP] in your web browser (replace [SERVER-IP] with your server's LAN IP)
2. **Students**: Use the main interface to borrow and return equipment
3. **Admins**: Click the admin button and login to manage the system

> **🔗 Access Requirements:** Make sure you are connected to **wifi@iiith** or have **OpenVPN** configured to access the IIIT network before using the system.

Note: mDNS/Bonjour references have been removed. Use the server IP address to access the application.

---

## 🔧 Management Commands

```bash
./setup.sh             # First-time setup (builds Docker images)
./start.sh             # Start all services
./stop.sh              # Stop all services  
./logs.sh              # View system logs
```

---

<div align="center" style="border-top: 2px solid #f2cdcd; padding-top: 20px; margin-top: 40px;">
  <p style="color: #cdd6f4;">Created with ❤️ for Robotics Research Centre</p>
  <p>
    <a href="https://github.com/Srindot" style="color: #f2cdcd; text-decoration: none;">👨‍💻 Developer</a> | 
    <a href="https://github.com/catppuccin/catppuccin" style="color: #f2cdcd; text-decoration: none;">🎨 Theme</a>
  </p>
</div>
