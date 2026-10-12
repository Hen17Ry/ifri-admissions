import '@fontsource/lexend/latin-600.css'
import '@fontsource/lexend/latin-700.css'
import '@fontsource/source-sans-3/latin-400.css'
import '@fontsource/source-sans-3/latin-600.css'
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from '@/app/router'
import '@/styles/main.css'

createApp(App).use(createPinia()).use(router).mount('#app')
