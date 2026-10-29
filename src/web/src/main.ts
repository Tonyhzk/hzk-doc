import { createApp } from 'vue'

import App from './App.vue'
import router from './router'
import './composables/useTheme'
import './styles/main.css'

createApp(App).use(router).mount('#app')
