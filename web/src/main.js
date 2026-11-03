import {createApp} from 'vue'
import {createRouter, createWebHistory} from 'vue-router'
import App from './App.vue'
import FacesView from './views/FacesView.vue'
import LiveView from './views/LiveView.vue'
import LoginView from './views/LoginView.vue'
import AdminView from './views/AdminView.vue'
import MeView from './views/MeView.vue'
import './style.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {path: '/', redirect: '/faces'},
    {path: '/faces', component: FacesView},
    {path: '/live', component: LiveView},
    {path: '/login', component: LoginView},
    {path: '/admin', component: AdminView},
    {path: '/me', component: MeView},
  ],
})

createApp(App).use(router).mount('#app')
