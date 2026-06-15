// Функция для экранирования HTML (защита от XSS)
function escapeHtml(text) {
    if (!text) return '';
    const map = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#039;'
    };
    return String(text).replace(/[&<>"']/g, m => map[m]);
}

// Безопасное получение токена
let authToken = null;
try {
    const token = localStorage.getItem('authToken');
    if (token && typeof token === 'string' && token.length > 0) {
        authToken = token;
    }
} catch (e) {
    console.error('Failed to get auth token:', e);
}


// Инициализация админ-панели — выбираем активный таб и грузим его.
function initAdminPanel() {
    switchTab('dashboard');
}

// Утилита: единая обёртка над fetch с авторизацией и обработкой 401.
async function adminFetch(url, options = {}) {
    const opts = {
        ...options,
        headers: {
            ...(options.headers || {}),
            'Authorization': `Bearer ${authToken}`,
        },
    };
    const response = await fetch(url, opts);
    if (response.status === 401) {
        logoutAdmin();
        throw new Error('unauthorized');
    }
    return response;
}

function logoutAdmin() {
    localStorage.removeItem('authToken');
    authToken = null;
    document.getElementById('loginSection').style.display = 'block';
    document.getElementById('adminSection').style.display = 'none';
}

// Вход
document.getElementById('loginForm')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const formData = new FormData(e.target);
    
    try {
        const response = await fetch('/api/admin/login', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                username: formData.get('username'),
                password: formData.get('password')
            })
        });
        
        if (response.ok) {
            const data = await response.json();
            authToken = data.token;
            localStorage.setItem('authToken', authToken);
            document.getElementById('loginSection').style.display = 'none';
            document.getElementById('adminSection').style.display = 'block';
            initAdminPanel();
        } else {
            alert('Неверный логин или пароль');
        }
    } catch (error) {
        console.error('Login error:', error);
        alert('Ошибка при входе');
    }
});

// Загрузка акций
async function loadPromotions() {
    try {
        const response = await fetch('/api/admin/promotions', {
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.status === 401) {
            localStorage.removeItem('authToken');
            authToken = null;
            document.getElementById('loginSection').style.display = 'block';
            document.getElementById('adminSection').style.display = 'none';
            return;
        }
        
        const data = await response.json();
        const listEl = document.getElementById('promotionsList');
        
        if (data.promotions && data.promotions.length > 0) {
            listEl.innerHTML = data.promotions.map(promotion => {
                const title = escapeHtml(promotion.promotions_title || '');
                const desc = promotion.promotions_description ? escapeHtml(promotion.promotions_description) : '';
                const discount = parseFloat(promotion.promotions_discount || 0).toFixed(2);
                const id = parseInt(promotion.promotions_id_pk || 0);
                const isActive = promotion.promotions_active !== false;
                
                return `
                <div class="promotion-item">
                    <div>
                        <h3>${title}</h3>
                        ${desc ? `<p>${desc}</p>` : ''}
                        <p>Скидка: ${discount}%</p>
                        <p>Статус: ${isActive ? 'Активна' : 'Неактивна'}</p>
                    </div>
                    <div class="promotion-actions">
                        <button class="btn btn-edit" onclick="editPromotion(${id})">Редактировать</button>
                        <button class="btn btn-danger" onclick="deletePromotion(${id})">Удалить</button>
                    </div>
                </div>
            `;
            }).join('');
        } else {
            listEl.innerHTML = '<p>Акций пока нет</p>';
        }
    } catch (error) {
        console.error('Error loading promotions:', error);
    }
}

// Показать форму создания акции
function showCreatePromotionForm() {
    document.getElementById('promotionModalTitle').textContent = 'Создать акцию';
    document.getElementById('promotionForm').reset();
    document.getElementById('promotionId').value = '';
    document.getElementById('promotionModal').style.display = 'block';
}

// Редактирование акции
async function editPromotion(id) {
    try {
        const response = await fetch(`/api/admin/promotions/${id}`, {
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.ok) {
            const promotion = await response.json();
            document.getElementById('promotionModalTitle').textContent = 'Редактировать акцию';
            document.getElementById('promotionId').value = promotion.promotions_id_pk;
            document.getElementById('promotionTitle').value = promotion.promotions_title;
            document.getElementById('promotionDescription').value = promotion.promotions_description || '';
            document.getElementById('promotionDiscount').value = promotion.promotions_discount;
            document.getElementById('promotionImageURL').value = promotion.promotions_image_url || '';
            document.getElementById('promotionActive').checked = promotion.promotions_active;
            
            if (promotion.promotions_start_date) {
                const startDate = new Date(promotion.promotions_start_date);
                document.getElementById('promotionStartDate').value = startDate.toISOString().slice(0, 16);
            }
            if (promotion.promotions_end_date) {
                const endDate = new Date(promotion.promotions_end_date);
                document.getElementById('promotionEndDate').value = endDate.toISOString().slice(0, 16);
            }
            
            document.getElementById('promotionModal').style.display = 'block';
        }
    } catch (error) {
        console.error('Error loading promotion:', error);
    }
}

// Удаление акции
async function deletePromotion(id) {
    if (!confirm('Вы уверены, что хотите удалить эту акцию?')) {
        return;
    }
    
    try {
        const response = await fetch(`/api/admin/promotions/${id}`, {
            method: 'DELETE',
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.ok) {
            loadPromotions();
        } else {
            alert('Ошибка при удалении акции');
        }
    } catch (error) {
        console.error('Error deleting promotion:', error);
        alert('Ошибка при удалении акции');
    }
}

// Сохранение акции
document.getElementById('promotionForm')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    
    const id = document.getElementById('promotionId').value;
    const promotion = {
        promotions_title: document.getElementById('promotionTitle').value,
        promotions_description: document.getElementById('promotionDescription').value || null,
        promotions_discount: parseFloat(document.getElementById('promotionDiscount').value) || 0,
        promotions_image_url: document.getElementById('promotionImageURL').value || null,
        promotions_active: document.getElementById('promotionActive').checked,
        promotions_start_date: document.getElementById('promotionStartDate').value || null,
        promotions_end_date: document.getElementById('promotionEndDate').value || null
    };
    
    try {
        const url = id 
            ? `/api/admin/promotions/${id}`
            : '/api/admin/promotions';
        const method = id ? 'PUT' : 'POST';
        
        const response = await fetch(url, {
            method: method,
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${authToken}`
            },
            body: JSON.stringify(promotion)
        });
        
        if (response.ok) {
            closePromotionModal();
            loadPromotions();
        } else {
            const error = await response.json();
            alert('Ошибка: ' + (error.error || 'Не удалось сохранить акцию'));
        }
    } catch (error) {
        console.error('Error saving promotion:', error);
        alert('Ошибка при сохранении акции');
    }
});

// Закрыть модальное окно
function closePromotionModal() {
    document.getElementById('promotionModal').style.display = 'none';
}

// Закрытие модального окна при клике вне его
window.onclick = function(event) {
    const modal = document.getElementById('promotionModal');
    if (event.target === modal) {
        closePromotionModal();
    }
}

// Синхронизация товаров с МойСклад
async function syncProducts() {
    const btn = document.getElementById('syncBtn');
    const resultDiv = document.getElementById('syncResult');
    
    btn.disabled = true;
    btn.textContent = '⏳ Синхронизация...';
    resultDiv.innerHTML = '<p>⏳ Синхронизация товаров из МойСклад...</p>';
    
    try {
        const response = await fetch('/api/admin/products/sync', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.status === 401) {
            localStorage.removeItem('authToken');
            authToken = null;
            document.getElementById('loginSection').style.display = 'block';
            document.getElementById('adminSection').style.display = 'none';
            resultDiv.innerHTML = '<p style="color: red;">Сессия истекла. Войдите заново.</p>';
            return;
        }
        
        if (!response.ok) {
            const error = await response.json();
            resultDiv.innerHTML = `<p style="color: red;">❌ Ошибка: ${error.error || error.message || 'Не удалось синхронизировать товары'}</p>`;
            return;
        }
        
        const data = await response.json();
        const now = new Date().toLocaleTimeString('ru-RU');
        resultDiv.innerHTML = `
            <div style="background: #e8f5e9; padding: 1rem; border-radius: 5px; margin-top: 1rem;">
                <p style="color: green; font-weight: bold;">✅ Синхронизация завершена! (${now})</p>
                <p><strong>Создано:</strong> ${data.result.created} товаров</p>
                <p><strong>Обновлено:</strong> ${data.result.updated} товаров</p>
                ${data.result.errors > 0 ? `<p style="color: orange;"><strong>Ошибок:</strong> ${data.result.errors}</p>` : ''}
            </div>
        `;
    } catch (error) {
        console.error('Sync error:', error);
        resultDiv.innerHTML = '<p style="color: red;">❌ Ошибка при синхронизации. Проверьте настройки МойСклад и логи приложения.</p>';
    } finally {
        btn.disabled = false;
        btn.textContent = '🔄 Обновить товары сейчас';
    }
}

// ===== Полная синхронизация =====
async function syncProductsFull() {
    const btn = document.getElementById('syncFullBtn');
    const resultDiv = document.getElementById('syncResult');
    if (!confirm('Полная пересинхронизация с МойСклад. Синхронизация запустится в фоне (2-5 мин). Продолжить?')) return;
    btn.disabled = true;
    btn.textContent = '⏳ Запускаем...';
    resultDiv.innerHTML = '<p>⏳ Отправляем запрос...</p>';
    try {
        const response = await adminFetch('/api/admin/products/sync/full', { method: 'POST' });
        const data = await response.json();
        if (response.status === 409) {
            resultDiv.innerHTML = `<p style="color: orange;">⚠️ ${escapeHtml(data.message || 'Синхронизация уже запущена, подождите.')}</p>`;
            return;
        }
        if (!response.ok) {
            resultDiv.innerHTML = `<p style="color: red;">❌ ${escapeHtml(data.error || data.message || 'Не удалось запустить синхронизацию')}</p>`;
            return;
        }
        // 202 Accepted — sync идёт в фоне
        const now = new Date().toLocaleTimeString('ru-RU');
        resultDiv.innerHTML = `
            <div style="background:#e8f5e9;padding:1rem;border-radius:5px;margin-top:1rem;">
                <p style="color:green;font-weight:bold;">✅ Синхронизация запущена в фоне (${now})</p>
                <p>Товары обновятся в течение 2–5 минут. Результат появится в логах сервера.</p>
            </div>`;
    } catch (e) {
        if (e.message !== 'unauthorized') {
            resultDiv.innerHTML = '<p style="color: red;">❌ Ошибка сети при запуске синхронизации.</p>';
        }
    } finally {
        btn.disabled = false;
        btn.textContent = '🔁 Полная пересинхронизация';
    }
}

// =============================================================================
// ТАБЫ
// =============================================================================
const tabLoaders = {
    dashboard: loadDashboard,
    orders: loadOrders,
    users: loadUsers,
    promotions: loadPromotions,
    sync: () => {}, // Sync — статичный, ничего грузить не надо
    metrics: loadMetrics,
};

function switchTab(name) {
    document.querySelectorAll('.tab-btn').forEach(b => {
        b.classList.toggle('active', b.dataset.tab === name);
    });
    document.querySelectorAll('.tab-content').forEach(s => {
        const active = s.id === `tab-${name}`;
        s.classList.toggle('active', active);
        // Управляем видимостью напрямую: у секций стоит инлайн style="display:none",
        // который перебивает любой класс из стайлшита — поэтому переключаем display здесь.
        s.style.display = active ? '' : 'none';
    });
    const loader = tabLoaders[name];
    if (loader) loader();
}

// =============================================================================
// УТИЛИТЫ ФОРМАТИРОВАНИЯ
// =============================================================================
function fmtMoney(v) {
    const n = Number(v) || 0;
    return n.toLocaleString('ru-RU', { minimumFractionDigits: 0, maximumFractionDigits: 2 }) + ' ₽';
}

function fmtDate(s) {
    if (!s) return '—';
    try {
        return new Date(s).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' });
    } catch (e) { return '—'; }
}

function fmtDateOnly(s) {
    if (!s) return '—';
    try {
        return new Date(s).toLocaleDateString('ru-RU');
    } catch (e) { return '—'; }
}

function statusBadge(status) {
    const labels = {
        pending: '⏳ Ожидание',
        completed: '✅ Выкуплен',
        cancelled: '❌ Отменён',
        expired: '⌛ Истёк',
    };
    const label = labels[status] || status;
    return `<span class="status-badge status-${escapeHtml(status)}">${escapeHtml(label)}</span>`;
}

// Преобразует "today/week/month/all" в RFC3339 from/to для API.
function periodToRange(period) {
    const now = new Date();
    let from = null;
    if (period === 'today') {
        from = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    } else if (period === 'week') {
        from = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
    } else if (period === 'month') {
        from = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
    }
    return {
        from: from ? from.toISOString() : null,
        to: null,
    };
}

// =============================================================================
// ДАШБОРД
// =============================================================================
async function loadDashboard() {
    await Promise.all([loadDashboardStats(), loadTodayOrders()]);
}

async function loadDashboardStats() {
    const period = document.getElementById('dashPeriod').value;
    const range = periodToRange(period);
    const params = new URLSearchParams();
    if (range.from) params.set('from', range.from);
    if (range.to) params.set('to', range.to);

    const grid = document.getElementById('statsGrid');
    grid.innerHTML = '<p class="text-muted">Загрузка статистики...</p>';

    try {
        const resp = await adminFetch('/api/admin/orders/stats?' + params.toString());
        if (!resp.ok) {
            grid.innerHTML = '<p style="color: red;">Не удалось загрузить статистику</p>';
            return;
        }
        const data = await resp.json();
        const s = data.by_status || {};
        const noShowPercent = ((data.no_show_rate || 0) * 100).toFixed(1);
        grid.innerHTML = `
            <div class="stat-card">
                <div class="stat-label">Всего заказов</div>
                <div class="stat-value">${data.total || 0}</div>
            </div>
            <div class="stat-card stat-warn">
                <div class="stat-label">⏳ Ожидание</div>
                <div class="stat-value">${s.pending || 0}</div>
            </div>
            <div class="stat-card stat-good">
                <div class="stat-label">✅ Выкуплены</div>
                <div class="stat-value">${s.completed || 0}</div>
            </div>
            <div class="stat-card stat-bad">
                <div class="stat-label">❌ Не выкуплены</div>
                <div class="stat-value">${data.no_show || 0}</div>
                <div class="stat-sub">${noShowPercent}% от всех</div>
            </div>
            <div class="stat-card stat-good">
                <div class="stat-label">💰 Выручка</div>
                <div class="stat-value">${fmtMoney(data.revenue)}</div>
                <div class="stat-sub">по выкупленным</div>
            </div>
            <div class="stat-card">
                <div class="stat-label">🧾 Средний чек</div>
                <div class="stat-value">${fmtMoney(data.avg_check)}</div>
            </div>`;
    } catch (e) {
        if (e.message !== 'unauthorized') {
            grid.innerHTML = '<p style="color: red;">Ошибка загрузки статистики</p>';
        }
    }
}

async function loadTodayOrders() {
    const wrap = document.getElementById('todayOrders');
    wrap.innerHTML = '<p class="text-muted">Загрузка...</p>';
    try {
        const resp = await adminFetch('/api/admin/orders/today');
        if (!resp.ok) {
            wrap.innerHTML = '<p style="color: red;">Не удалось загрузить</p>';
            return;
        }
        const data = await resp.json();
        if (!data.items || data.items.length === 0) {
            wrap.innerHTML = '<p class="text-muted">На сегодня броней нет</p>';
            return;
        }
        wrap.innerHTML = renderOrdersTable(data.items);
    } catch (e) {
        if (e.message !== 'unauthorized') {
            wrap.innerHTML = '<p style="color: red;">Ошибка</p>';
        }
    }
}

// =============================================================================
// МЕТРИКИ ПРИЛОЖЕНИЯ
// =============================================================================
function fmtInt(v) {
    return Math.round(Number(v) || 0).toLocaleString('ru-RU');
}

function fmtPct(v) {
    return ((Number(v) || 0) * 100).toFixed(1) + '%';
}

function fmtUptime(sec) {
    sec = Math.floor(Number(sec) || 0);
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return `${d}д ${h}ч ${m}м`;
    if (h > 0) return `${h}ч ${m}м`;
    return `${m}м`;
}

function metricCard(label, value, sub, mod) {
    const subHtml = sub ? `<div class="stat-sub">${sub}</div>` : '';
    return `
        <div class="stat-card ${mod || ''}">
            <div class="stat-label">${label}</div>
            <div class="stat-value">${value}</div>
            ${subHtml}
        </div>`;
}

async function loadMetrics() {
    const wrap = document.getElementById('metricsContent');
    wrap.innerHTML = '<p class="text-muted" style="padding:var(--sp-3)">Загрузка...</p>';
    try {
        const resp = await adminFetch('/api/admin/metrics');
        if (!resp.ok) {
            wrap.innerHTML = '<p style="color: red;">Не удалось загрузить метрики</p>';
            return;
        }
        const m = await resp.json();
        const http = m.http || {};
        const orders = m.orders || {};
        const ms = m.moysklad || {};
        const cache = m.cache || {};
        const pool = m.db_pool || {};
        const rt = m.runtime || {};
        const cls = http.by_class || {};

        const section = (title, cards) => `
            <h3 class="today-orders-title">${title}</h3>
            <div class="stats-grid">${cards.join('')}</div>`;

        wrap.innerHTML = `
            <p class="admin-section-sub">⏱ Аптайм: <b>${fmtUptime(m.uptime_seconds)}</b></p>
            ${section('🌐 HTTP', [
                metricCard('Всего запросов', fmtInt(http.total)),
                metricCard('Сейчас в обработке', fmtInt(http.in_flight)),
                metricCard('Средняя задержка', (Number(http.avg_latency_ms) || 0).toFixed(1) + ' мс'),
                metricCard('Ошибки 5xx', fmtInt(http.errors_5xx), `2xx: ${fmtInt(cls['2xx'])} · 4xx: ${fmtInt(cls['4xx'])}`, Number(http.errors_5xx) > 0 ? 'stat-bad' : 'stat-good'),
            ])}
            ${section('🛒 Заказы (с момента старта)', [
                metricCard('Создано', fmtInt(orders.created), null, 'stat-good'),
                metricCard('Выкуплено', fmtInt(orders.completed)),
                metricCard('Отменено', fmtInt(orders.cancelled)),
                metricCard('Истекло', fmtInt(orders.expired), null, 'stat-warn'),
                metricCard('Сумма заказов', fmtMoney(orders.revenue)),
            ])}
            ${section('🗄 Кэш остатков', [
                metricCard('Попадания', fmtInt(cache.hits), null, 'stat-good'),
                metricCard('Промахи', fmtInt(cache.misses)),
                metricCard('Hit-rate', fmtPct(cache.hit_rate)),
            ])}
            ${section('🔄 Синхронизация МойСклад', [
                metricCard('Успешных прогонов', fmtInt(ms.sync_success), null, 'stat-good'),
                metricCard('Ошибок', fmtInt(ms.sync_error), null, Number(ms.sync_error) > 0 ? 'stat-bad' : ''),
                metricCard('Средняя длительность', (Number(ms.avg_duration_seconds) || 0).toFixed(1) + ' с'),
            ])}
            ${section('⚙️ Ресурсы', [
                metricCard('Пул БД (занято/всего)', `${fmtInt(pool.in_use)} / ${fmtInt(pool.total)}`, `свободно: ${fmtInt(pool.idle)}`),
                metricCard('Горутины', fmtInt(rt.goroutines)),
                metricCard('Память (alloc)', (Number(rt.alloc_mb) || 0).toFixed(1) + ' МБ'),
                metricCard('Сборок мусора', fmtInt(rt.num_gc)),
            ])}`;
    } catch (e) {
        if (e.message !== 'unauthorized') {
            wrap.innerHTML = '<p style="color: red;">Ошибка загрузки метрик</p>';
        }
    }
}

// =============================================================================
// СПИСОК ЗАКАЗОВ
// =============================================================================
let ordersOffset = 0;
const ORDERS_LIMIT = 50;
let ordersTotal = 0;

function resetOrderFilters() {
    document.getElementById('ordersSearch').value = '';
    document.getElementById('ordersStatus').value = '';
    document.getElementById('ordersFrom').value = '';
    document.getElementById('ordersTo').value = '';
    document.getElementById('ordersSort').value = 'created_at';
    ordersOffset = 0;
    loadOrders();
}

function ordersPage(delta) {
    const next = ordersOffset + delta * ORDERS_LIMIT;
    if (next < 0) return;
    if (next >= ordersTotal && delta > 0) return;
    ordersOffset = next;
    loadOrders();
}

async function loadOrders() {
    const wrap = document.getElementById('ordersTable');
    wrap.innerHTML = '<p class="text-muted">Загрузка...</p>';

    const params = new URLSearchParams();
    const search = document.getElementById('ordersSearch').value.trim();
    const status = document.getElementById('ordersStatus').value;
    const from = document.getElementById('ordersFrom').value;
    const to = document.getElementById('ordersTo').value;
    const sort = document.getElementById('ordersSort').value;

    if (search) params.set('search', search);
    if (status) params.set('status', status);
    if (from) params.set('from', new Date(from).toISOString());
    if (to) params.set('to', new Date(to + 'T23:59:59').toISOString());
    if (sort) params.set('sort', sort);
    params.set('order', 'desc');
    params.set('limit', ORDERS_LIMIT);
    params.set('offset', ordersOffset);

    try {
        const resp = await adminFetch('/api/admin/orders?' + params.toString());
        if (!resp.ok) {
            wrap.innerHTML = '<p style="color: red;">Не удалось загрузить заказы</p>';
            return;
        }
        const data = await resp.json();
        ordersTotal = data.total || 0;
        if (!data.items || data.items.length === 0) {
            wrap.innerHTML = '<p class="text-muted">Заказов не найдено</p>';
        } else {
            wrap.innerHTML = renderOrdersTable(data.items);
        }
        const pageStart = ordersOffset + 1;
        const pageEnd = Math.min(ordersOffset + ORDERS_LIMIT, ordersTotal);
        document.getElementById('ordersPageInfo').textContent =
            ordersTotal === 0 ? '—' : `${pageStart}–${pageEnd} из ${ordersTotal}`;
        document.getElementById('ordersPrev').disabled = ordersOffset <= 0;
        document.getElementById('ordersNext').disabled = ordersOffset + ORDERS_LIMIT >= ordersTotal;
    } catch (e) {
        if (e.message !== 'unauthorized') {
            wrap.innerHTML = '<p style="color: red;">Ошибка загрузки</p>';
        }
    }
}

function renderOrdersTable(items) {
    const rows = items.map(o => {
        const id = parseInt(o.orders_id_pk || o.id || 0);
        const created = fmtDate(o.orders_created_at || o.created_at);
        const reservedUntil = fmtDate(o.orders_reserved_until || o.reserved_until);
        const pickupAt = fmtDate(o.orders_pickup_at || o.pickup_at);
        const customerName = escapeHtml(o.orders_customer_name || o.customer_name || '—');
        const phone = escapeHtml(o.orders_phone || o.phone || '—');
        const total = fmtMoney(o.orders_total_price || o.total_price);
        const status = o.orders_status || o.status || 'pending';
        return `
            <tr onclick="openOrderModal(${id})">
                <td><strong>#${id}</strong></td>
                <td>${created}</td>
                <td>${customerName}</td>
                <td>${phone}</td>
                <td>${total}</td>
                <td>${statusBadge(status)}</td>
                <td>${reservedUntil}</td>
                <td>${pickupAt}</td>
            </tr>`;
    }).join('');
    return `
        <table class="admin-table">
            <thead>
                <tr>
                    <th>ID</th>
                    <th>Создан</th>
                    <th>Клиент</th>
                    <th>Телефон</th>
                    <th>Сумма</th>
                    <th>Статус</th>
                    <th>Бронь до</th>
                    <th>Заберёт в</th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>`;
}

// =============================================================================
// КАРТОЧКА ЗАКАЗА
// =============================================================================
let currentOrderId = null;

async function openOrderModal(orderID) {
    currentOrderId = orderID;
    const modal = document.getElementById('orderModal');
    const body = document.getElementById('orderModalBody');
    modal.style.display = 'block';
    body.innerHTML = '<p class="text-muted">Загрузка карточки заказа...</p>';

    try {
        const resp = await adminFetch(`/api/admin/orders/${orderID}`);
        if (!resp.ok) {
            body.innerHTML = '<p style="color: red;">Не удалось загрузить заказ</p>';
            return;
        }
        const data = await resp.json();
        body.innerHTML = renderOrderDetails(data);
    } catch (e) {
        if (e.message !== 'unauthorized') {
            body.innerHTML = '<p style="color: red;">Ошибка загрузки</p>';
        }
    }
}

function closeOrderModal() {
    document.getElementById('orderModal').style.display = 'none';
    currentOrderId = null;
}

function renderOrderDetails(data) {
    const o = data.order || {};
    const items = data.items || [];
    const events = data.events || [];
    const id = o.orders_id_pk || o.id;
    const status = o.orders_status || o.status || 'pending';
    const isPending = status === 'pending';

    const itemsHtml = items.map(it => {
        const qty = it.order_items_quantity || it.Quantity || 0;
        const price = it.order_items_price || it.Price || 0;
        const productID = it.order_items_product_id_fk || it.ProductID;
        return `
            <div class="order-item-row">
                <div>Товар #${parseInt(productID)}</div>
                <div>${qty} × ${fmtMoney(price)}</div>
                <div><strong>${fmtMoney(qty * price)}</strong></div>
            </div>`;
    }).join('');

    const total = items.reduce((sum, it) => {
        const qty = it.order_items_quantity || it.Quantity || 0;
        const price = it.order_items_price || it.Price || 0;
        return sum + qty * price;
    }, 0);

    const eventsHtml = events.length === 0
        ? '<p class="text-muted">Событий нет</p>'
        : events.map(e => {
            const time = fmtDate(e.created_at || e.CreatedAt);
            const type = e.event_type || e.EventType || '';
            const actor = e.actor_user_id || e.ActorUserID;
            const payload = e.payload || e.Payload;
            const payloadHtml = payload && Object.keys(payload).length > 0
                ? `<div class="event-payload">${escapeHtml(JSON.stringify(payload))}</div>`
                : '';
            return `
                <div class="audit-event event-${escapeHtml(type)}">
                    <div class="event-time">${time}${actor ? ` · клиент/админ #${actor}` : ' · системное'}</div>
                    <div class="event-title">${escapeHtml(eventLabel(type))}</div>
                    ${payloadHtml}
                </div>`;
        }).join('');

    const actionsHtml = isPending ? `
        <div class="action-bar">
            <button class="btn btn-edit" onclick="shipOrder(${id})">✅ Клиент пришёл (выкуплен)</button>
            <button class="btn btn-danger" onclick="cancelOrderAdmin(${id})">❌ Отменить заказ</button>
            <button class="btn" style="background:#888;color:#fff" onclick="expireOrder(${id})">⌛ Истечь бронь</button>
        </div>` : '';

    return `
        <h2>Заказ #${id} ${statusBadge(status)}</h2>
        <div class="order-detail-grid">
            <div class="order-info-block">
                <h4>Создан</h4>
                <div class="info-value">${fmtDate(o.orders_created_at || o.created_at)}</div>
            </div>
            <div class="order-info-block">
                <h4>Бронь до</h4>
                <div class="info-value">${fmtDate(o.orders_reserved_until || o.reserved_until)}</div>
            </div>
            <div class="order-info-block">
                <h4>Клиент</h4>
                <div class="info-value">${escapeHtml(o.orders_customer_name || o.customer_name || '—')}</div>
            </div>
            <div class="order-info-block">
                <h4>Телефон</h4>
                <div class="info-value">${escapeHtml(o.orders_phone || o.phone || '—')}</div>
            </div>
            <div class="order-info-block">
                <h4>Адрес</h4>
                <div class="info-value">${escapeHtml(o.orders_address || o.address || '—')}</div>
            </div>
            <div class="order-info-block">
                <h4>МойСклад ID</h4>
                <div class="info-value">${escapeHtml(o.orders_moysklad_id || o.moysklad_id || '—')}</div>
            </div>
            <div class="order-info-block">
                <h4>🕐 Заберёт в</h4>
                <div class="info-value" style="font-weight:700;color:#4A7C59">${fmtDate(o.orders_pickup_at || o.pickup_at)}</div>
            </div>
        </div>

        <h3>Состав заказа</h3>
        <div class="order-items-list">
            ${itemsHtml || '<p class="text-muted">Позиций нет</p>'}
            <div class="order-item-row">
                <div>Итого:</div>
                <div></div>
                <div>${fmtMoney(total || o.orders_total_price || o.total_price)}</div>
            </div>
        </div>

        <h3>Лог событий</h3>
        <div class="audit-timeline">${eventsHtml}</div>

        ${actionsHtml}`;
}

function eventLabel(type) {
    const labels = {
        created: 'Заказ создан',
        moysklad_synced: 'Передан в МойСклад',
        moysklad_failed: 'Не удалось передать в МойСклад',
        shipped: 'Клиент выкупил',
        cancelled: 'Заказ отменён',
        expired: 'Бронь истекла',
    };
    return labels[type] || type;
}

async function shipOrder(orderID) {
    if (!confirm('Подтвердить выкуп? Резерв снимется в МойСклад.')) return;
    try {
        const resp = await adminFetch(`/api/admin/orders/${orderID}/ship`, { method: 'POST' });
        if (!resp.ok) { alert('Ошибка'); return; }
        closeOrderModal();
        loadOrders();
        loadDashboard();
    } catch (e) { /* 401 уже обработан */ }
}

async function cancelOrderAdmin(orderID) {
    if (!confirm('Отменить заказ? Товары вернутся на склад, заказ удалится в МойСклад.')) return;
    try {
        const resp = await adminFetch(`/api/admin/orders/${orderID}/cancel`, { method: 'POST' });
        if (!resp.ok) {
            const err = await resp.json().catch(() => ({}));
            alert('Ошибка отмены: ' + (err.error || resp.status));
            return;
        }
        closeOrderModal();
        loadOrders();
        loadDashboard();
    } catch (e) { /* 401 уже обработан */ }
}

async function expireOrder(orderID) {
    if (!confirm('Истечь бронь? Товары вернутся на склад.')) return;
    try {
        const resp = await adminFetch(`/api/admin/orders/${orderID}/expire`, { method: 'POST' });
        if (!resp.ok) { alert('Ошибка'); return; }
        closeOrderModal();
        loadOrders();
        loadDashboard();
    } catch (e) { /* 401 уже обработан */ }
}

// =============================================================================
// КЛИЕНТЫ
// =============================================================================
let usersOffset = 0;
const USERS_LIMIT = 50;
let usersTotal = 0;

function resetUserFilters() {
    document.getElementById('usersSearch').value = '';
    usersOffset = 0;
    loadUsers();
}

function usersPage(delta) {
    const next = usersOffset + delta * USERS_LIMIT;
    if (next < 0) return;
    if (next >= usersTotal && delta > 0) return;
    usersOffset = next;
    loadUsers();
}

async function loadUsers() {
    const wrap = document.getElementById('usersTable');
    wrap.innerHTML = '<p class="text-muted">Загрузка...</p>';

    const search = document.getElementById('usersSearch').value.trim();
    const params = new URLSearchParams();
    if (search) params.set('search', search);
    params.set('limit', USERS_LIMIT);
    params.set('offset', usersOffset);

    try {
        const resp = await adminFetch('/api/admin/users?' + params.toString());
        if (!resp.ok) {
            wrap.innerHTML = '<p style="color: red;">Не удалось загрузить</p>';
            return;
        }
        const data = await resp.json();
        usersTotal = data.total || 0;
        const items = data.items || [];
        if (items.length === 0) {
            wrap.innerHTML = '<p class="text-muted">Клиентов не найдено</p>';
        } else {
            wrap.innerHTML = renderUsersTable(items);
        }
        const pageStart = usersOffset + 1;
        const pageEnd = Math.min(usersOffset + USERS_LIMIT, usersTotal);
        document.getElementById('usersPageInfo').textContent =
            usersTotal === 0 ? '—' : `${pageStart}–${pageEnd} из ${usersTotal}`;
        document.getElementById('usersPrev').disabled = usersOffset <= 0;
        document.getElementById('usersNext').disabled = usersOffset + USERS_LIMIT >= usersTotal;
    } catch (e) {
        if (e.message !== 'unauthorized') {
            wrap.innerHTML = '<p style="color: red;">Ошибка</p>';
        }
    }
}

function renderUsersTable(items) {
    const rows = items.map(u => `
        <tr onclick="openUserModal(${parseInt(u.id)})">
            <td><strong>#${parseInt(u.id)}</strong></td>
            <td>${escapeHtml(u.username || '')}</td>
            <td>${escapeHtml(u.full_name || '—')}</td>
            <td>${escapeHtml(u.email || '')} ${u.email_verified ? '✓' : ''}</td>
            <td>${escapeHtml(u.phone || '—')}</td>
            <td>${u.orders_count || 0}</td>
            <td>${fmtDateOnly(u.last_order_at)}</td>
            <td>${fmtDateOnly(u.created_at)}</td>
        </tr>`).join('');
    return `
        <table class="admin-table">
            <thead>
                <tr>
                    <th>ID</th>
                    <th>Логин</th>
                    <th>Имя</th>
                    <th>Email</th>
                    <th>Телефон</th>
                    <th>Заказов</th>
                    <th>Последний</th>
                    <th>Регистрация</th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>`;
}

// =============================================================================
// КАРТОЧКА КЛИЕНТА
// =============================================================================
async function openUserModal(userID) {
    const modal = document.getElementById('userModal');
    const body = document.getElementById('userModalBody');
    modal.style.display = 'block';
    body.innerHTML = '<p class="text-muted">Загрузка...</p>';

    try {
        const resp = await adminFetch(`/api/admin/users/${userID}/stats`);
        if (!resp.ok) {
            body.innerHTML = '<p style="color: red;">Не удалось загрузить</p>';
            return;
        }
        const data = await resp.json();
        body.innerHTML = renderUserDetails(data);
    } catch (e) {
        if (e.message !== 'unauthorized') {
            body.innerHTML = '<p style="color: red;">Ошибка</p>';
        }
    }
}

function closeUserModal() {
    document.getElementById('userModal').style.display = 'none';
}

function renderUserDetails(data) {
    const u = data.user || {};
    const s = data.stats || {};
    const top = data.top_products || [];

    const noShow = ((s.no_show_rate || 0) * 100).toFixed(1);
    const topHtml = top.length === 0
        ? '<p class="text-muted">Нет выкупленных заказов</p>'
        : `<table class="admin-table"><thead><tr>
                <th>Товар</th><th>Заказов</th><th>Куплено</th><th>Потрачено</th>
            </tr></thead><tbody>
            ${top.map(t => `
                <tr>
                    <td>${escapeHtml(t.product_name || '—')}</td>
                    <td>${t.order_count || 0}</td>
                    <td>${t.total_qty || 0}</td>
                    <td>${fmtMoney(t.total_spent)}</td>
                </tr>`).join('')}
            </tbody></table>`;

    return `
        <h2>${escapeHtml(u.full_name || u.username || 'Клиент')} <small>#${parseInt(u.id)}</small></h2>
        <div class="order-detail-grid">
            <div class="order-info-block">
                <h4>Логин</h4>
                <div class="info-value">${escapeHtml(u.username || '—')}</div>
            </div>
            <div class="order-info-block">
                <h4>Email</h4>
                <div class="info-value">${escapeHtml(u.email || '—')} ${u.email_verified ? '✓' : ''}</div>
            </div>
            <div class="order-info-block">
                <h4>Телефон</h4>
                <div class="info-value">${escapeHtml(u.phone || '—')}</div>
            </div>
            <div class="order-info-block">
                <h4>Регистрация</h4>
                <div class="info-value">${fmtDate(u.created_at)}</div>
            </div>
        </div>

        <h3>Статистика</h3>
        <div class="stats-grid">
            <div class="stat-card">
                <div class="stat-label">Всего заказов</div>
                <div class="stat-value">${s.total_orders || 0}</div>
            </div>
            <div class="stat-card stat-good">
                <div class="stat-label">Выкуплено</div>
                <div class="stat-value">${s.completed_count || 0}</div>
            </div>
            <div class="stat-card stat-bad">
                <div class="stat-label">Не выкуплено</div>
                <div class="stat-value">${(s.cancelled_count || 0) + (s.expired_count || 0)}</div>
                <div class="stat-sub">${noShow}% no-show</div>
            </div>
            <div class="stat-card">
                <div class="stat-label">Выручка</div>
                <div class="stat-value" style="font-size: 1.4rem;">${fmtMoney(s.total_revenue)}</div>
                <div class="stat-sub">средний чек ${fmtMoney(s.avg_check)}</div>
            </div>
        </div>

        <h3>Топ товаров клиента</h3>
        ${topHtml}`;
}

// Закрытие модалок по клику вне контента.
window.addEventListener('click', function(event) {
    const orderModal = document.getElementById('orderModal');
    const userModal = document.getElementById('userModal');
    if (event.target === orderModal) closeOrderModal();
    if (event.target === userModal) closeUserModal();
});

// Проверка авторизации — запускается после объявления всех переменных и функций.
if (authToken) {
    document.getElementById('loginSection').style.display = 'none';
    document.getElementById('adminSection').style.display = 'block';
    initAdminPanel();
}

