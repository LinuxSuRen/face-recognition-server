<script setup>
</script>

<template>
  <header class="topbar">
    <span class="brand">人脸识别服务</span>
    <nav>
      <router-link to="/faces">人员与人脸</router-link>
      <router-link to="/live">实时识别</router-link>
    </nav>
  </header>
  <main class="page">
    <router-view />
  </main>
</template>
