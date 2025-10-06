# 🤖 RRC Inventory Management System

<div align="center">
  <img src="rrc_logo.png" alt="RRC Logo" width="200"/>
</div>

<div align="center">
  <h3>Simple and Modern Lab Equipment Management</h3>
  <p>A web application for managing Robotics Research Centre lab equipment inventory</p>
</div>

---

## 🚀 Quick Start

### Prerequisites
- Docker and Docker Compose installed on your system

### Getting Started

1. **Start the application:**
   ```bash
   ./start.sh
   ```

2. **Access the system:**
   - 🌐 **Main Application**: http://localhost
   - 👨‍💼 **Admin Login**: Username: `Srinath`, Password: `rrc@srinath`

3. **Stop the application:**
   ```bash
   ./stop.sh
   ```

That's it! The system is now ready to use.

---

## �️ Technology Stack

- **Frontend**: SvelteKit with TypeScript
- **Backend**: Go with Gin framework  
- **Database**: PostgreSQL
- **Deployment**: Docker & Docker Compose

---

## � Features

### For Students & Staff
- **Borrow Equipment**: Submit requests with photos and details
- **Return Items**: Mark items as returned when done
- **Track Status**: View all your borrowed items and their status

### For Administrators
- **Approve Requests**: Review and approve/deny borrow requests
- **Manage Returns**: Process return requests and mark items as found/missing
- **View History**: Complete searchable history of all equipment
- **Admin Management**: Add/remove administrators (Super Admin only)

---

## 📝 Usage

1. **Visit** http://localhost in your web browser
2. **Students**: Use the main interface to borrow and return equipment
3. **Admins**: Click the admin button and login to manage the system

---

## 🔧 Management Commands

```bash
./start.sh      # Start all services
./stop.sh       # Stop all services  
./logs.sh       # View system logs
```

---

## 🆘 Need Help?

If you encounter any issues:

1. **Check logs**: Run `./logs.sh` to see what's happening
2. **Restart system**: Run `./stop.sh` then `./start.sh`
3. **Contact**: Reach out to the system administrator

---

<div align="center">
  <p>Created with ❤️ for Robotics Research Centre</p>
  <p>
    <a href="https://github.com/Srindot">👨‍💻 Developer</a> | 
    <a href="https://github.com/catppuccin/catppuccin">🎨 Theme</a>
  </p>
</div>