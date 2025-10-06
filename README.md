# RRC Inventory Management System

<div align="center">
  <img src="rrc_logo.png" alt="RRC Logo" width="200"/>
</div>

<div align="center">
  <h3>🎯 Simple and Modern Lab Equipment Management</h3>
  <p style="color: #f2cdcd;">A web application for managing Robotics Research Centre lab equipment inventory</p>
</div>

<div align="center">
  
![Catppuccin](https://img.shields.io/badge/catppuccin-mocha-f2cdcd?style=for-the-badge&logo=catppuccin&logoColor=white)
![Docker](https://img.shields.io/badge/docker-ready-f2cdcd?style=for-the-badge&logo=docker&logoColor=white)
![SvelteKit](https://img.shields.io/badge/sveltekit-frontend-f2cdcd?style=for-the-badge&logo=svelte&logoColor=white)
![Go](https://img.shields.io/badge/go-backend-f2cdcd?style=for-the-badge&logo=go&logoColor=white)

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
   - **Main Application**: [http://localhost](http://localhost)
   - **Admin Login**: Username: `Srinath`, Password: `rrc@srinath`

3. **Stop the application:**
   ```bash
   ./stop.sh
   ```

<div align="center" style="background: linear-gradient(135deg, #f2cdcd, #f5c2e7); color: #11111b; padding: 10px; border-radius: 8px; margin: 20px 0;">
  <strong>🎉 That's it! The system is now ready to use.</strong>
</div>

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

### 📚 For Students & Staff
- **Borrow Equipment**: Submit requests with photos and details
- **Return Items**: Mark items as returned when done
- **Track Status**: View all your borrowed items and their status

### 👑 For Administrators
- **Approve Requests**: Review and approve/deny borrow requests
- **Manage Returns**: Process return requests and mark items as found/missing
- **View History**: Complete searchable history of all equipment
- **Admin Management**: Add/remove administrators (Super Admin only)

---

## 📖 Usage

1. **Visit** [http://localhost](http://localhost) in your web browser
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

<div align="center" style="border-top: 2px solid #f2cdcd; padding-top: 20px; margin-top: 40px;">
  <p style="color: #cdd6f4;">Created with ❤️ for Robotics Research Centre</p>
  <p>
    <a href="https://github.com/Srindot" style="color: #f2cdcd; text-decoration: none;">👨‍💻 Developer</a> | 
    <a href="https://github.com/catppuccin/catppuccin" style="color: #f2cdcd; text-decoration: none;">🎨 Theme</a>
  </p>
</div>
