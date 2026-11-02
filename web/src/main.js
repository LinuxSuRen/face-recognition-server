import {createApp} from 'vue'
import {createRouter, createWebHistory} from 'vue-router'
import App from './App.vue'
import FacesView from './views/FacesView.vue'
import LiveView from './views/LiveView.vue'
import './style.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {path: '/', redirect: '/faces'},
    {path: '/faces', component: FacesView},
    {path: '/live', component: LiveView},
  ],
})

createApp(App).use(router).mount('#app')
