import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { sitesSchema, type Site } from '@/api/schemas'

// Общее состояние сайтов: список и страницы выбранного сайта читают одни и те же данные.
export const useSitesStore = defineStore('sites', () => {
  const sites = ref<Site[]>([])
  const limits = ref({ max_sites: 1, disk_quota_bytes: 0 })
  const domainConfig = ref({ available: false, server_ips: [] as string[], per_site: 0 })
  const logsAvailable = ref(false)
  const loading = ref(true)
  const loadError = ref('')

  const used = computed(() => sites.value.reduce((sum, s) => sum + s.disk_bytes, 0))

  /** Возвращает текст ошибки (пустой, если всё загрузилось). */
  async function load(fallback: string): Promise<void> {
    loadError.value = ''
    try {
      const r = await api('/api/sites', { schema: sitesSchema })
      sites.value = r.sites
      limits.value = r.limits
      domainConfig.value = r.domain_config
      logsAvailable.value = r.logs_available
    } catch (e) {
      loadError.value = e instanceof ApiError ? e.message : fallback
    } finally {
      loading.value = false
    }
  }

  const byId = (id: number) => sites.value.find((s) => s.id === id)

  /** Ждём ли мы выпуска сертификата (у сайта или у своего домена). */
  const waiting = computed(() =>
    sites.value.some(
      (s) => s.cert_status === 'pending' || s.domains.some((d) => d.status === 'pending_dns' || d.status === 'pending_cert'),
    ),
  )

  return { sites, limits, domainConfig, logsAvailable, loading, loadError, used, waiting, load, byId }
})
