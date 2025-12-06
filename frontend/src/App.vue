<template>
  <div class="app-container min-h-screen bg-gradient-to-br from-indigo-500 via-purple-500 to-pink-500">
    <nav class="navbar backdrop-blur-md">
      <div class="container navbar-content">
        <div class="logo">
          <i class="fa-solid fa-link"></i>
          <span>GoShort</span>
        </div>
        <div class="nav-links">
          <router-link to="/" class="nav-item">
            <i class="fa-solid fa-home"></i> Home
          </router-link>
          <router-link to="/stats" class="nav-item">
            <i class="fa-solid fa-chart-bar"></i> Stats
          </router-link>
        </div>
      </div>
    </nav>

    <main class="main-content">
      <router-view v-slot="{ Component }">
        <transition name="fade" mode="out-in">
          <component :is="Component" />
        </transition>
      </router-view>
    </main>

    <footer class="footer">
      <p>&copy; 2025 GoShort. Secure & Fast URL Shortener.</p>
    </footer>
  </div>
</template>

<script setup>
// Logic is handled by Router
</script>