<template>
  <div class="home-container container">
    <div class="hero-section text-center">
      <h1 class="title">Shorten Your URLs</h1>
      <p class="subtitle">Fast, secure, and powerful URL shortening service for modern needs.</p>
    </div>

    <div class="card glass-panel main-shortener">
      <div v-if="error" class="alert alert-error">
        <i class="fa-solid fa-circle-exclamation"></i> {{ error }}
      </div>

      <div v-if="shortUrl" class="alert alert-success">
        <div class="success-content">
          <i class="fa-solid fa-check-circle"></i>
          <span>Short link created:</span>
          <a :href="shortUrl" target="_blank" class="short-link">{{ shortUrl }}</a>
        </div>
        <button @click="copyToClipboard" class="btn-icon" title="Copy">
          <i class="fa-regular fa-copy"></i>
        </button>
      </div>

      <form @submit.prevent="shortenUrl" class="shortener-form">
        <div class="form-group">
          <label class="label"><i class="fa-solid fa-link"></i> Enter your long URL</label>
          <input v-model="longUrl" type="url" class="input" placeholder="https://example.com/very-long-url..." required>
        </div>

        <div class="form-group">
          <label class="label"><i class="fa-solid fa-tag"></i> Custom alias (Optional)</label>
          <input v-model="customAlias" type="text" class="input" placeholder="my-cool-link">
          <small class="hint">Only letters, numbers, and dashes allowed.</small>
        </div>

        <button type="submit" class="btn btn-primary btn-block" :disabled="loading">
          <span v-if="loading" class="loading"></span>
          <span v-else>Shorten URL <i class="fa-solid fa-wand-magic-sparkles"></i></span>
        </button>
      </form>
    </div>

    <div class="features-grid">
      <div class="feature-card glass-panel">
        <div class="icon-box"><i class="fa-solid fa-shield-halved"></i></div>
        <h3>Secure</h3><p>Protected against malware and phishing attacks automatically.</p>
      </div>
      <div class="feature-card glass-panel">
        <div class="icon-box"><i class="fa-solid fa-bolt"></i></div>
        <h3>Fast</h3><p>Lightning-fast redirects powered by Redis caching.</p>
      </div>
      <div class="feature-card glass-panel">
        <div class="icon-box"><i class="fa-solid fa-chart-line"></i></div>
        <h3>Analytics</h3><p>Track clicks and monitor your link performance in real-time.</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import api from '../services/api' 

const longUrl = ref('')
const customAlias = ref('')
const shortUrl = ref('')
const error = ref('')
const loading = ref(false)

const shortenUrl = async () => {
  loading.value = true
  error.value = ''
  shortUrl.value = ''

  try {
    const response = await api.post('/v1/shorten', {
      url: longUrl.value,
      short: customAlias.value
    })
    
    const code = response.data.short_code || response.data.result || response.data.short || response.data;
    
    if (code.toString().startsWith('http')) {
        shortUrl.value = code;
    } else {
        shortUrl.value = `${window.location.origin}/${code}`;
    }
    
  } catch (err) {
    console.error(err)
    if (err.response) {
       error.value = err.response.data.error || 'Failed to shorten URL.';
    } else {
       error.value = 'Network Error. Please check your connection.';
    }
  } finally {
    loading.value = false
  }
}

const copyToClipboard = () => {
  navigator.clipboard.writeText(shortUrl.value)
  alert('Copied to clipboard!')
}
</script>