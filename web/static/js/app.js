// ============================================================
// ХозДача — общий клиентский код
// Корзина (localStorage + drawer), header, toast, auth
// ============================================================

const App = (() => {
    const PLACEHOLDER_IMG =
        'data:image/svg+xml;utf8,' +
        encodeURIComponent(
            `<svg xmlns="http://www.w3.org/2000/svg" width="240" height="220" viewBox="0 0 240 220">
                <rect width="240" height="220" fill="#FFF8E1"/>
                <text x="120" y="115" font-family="sans-serif" font-size="14"
                      fill="#8D6E63" text-anchor="middle">нет фото</text>
            </svg>`
        );

    // ---- Утилиты ----
    const escapeHtml = (s) => {
        if (s == null) return '';
        return String(s).replace(/[&<>"']/g, (c) => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
    };

    const formatPrice = (n) => {
        const num = Number(n) || 0;
        return num.toLocaleString('ru-RU', { minimumFractionDigits: 0, maximumFractionDigits: 2 }) + ' ₽';
    };

    const safeJSON = (s, fallback) => {
        try { return JSON.parse(s) ?? fallback; } catch { return fallback; }
    };

    // ---- Toast ----
    function ensureToastContainer() {
        let c = document.querySelector('.toast-container');
        if (!c) {
            c = document.createElement('div');
            c.className = 'toast-container';
            document.body.appendChild(c);
        }
        return c;
    }
    function toast(message, type = 'info') {
        const c = ensureToastContainer();
        const t = document.createElement('div');
        t.className = `toast toast-${type}`;
        t.textContent = message;
        c.appendChild(t);
        setTimeout(() => t.remove(), 3000);
    }

    // ---- Auth ----
    const Auth = {
        token: () => localStorage.getItem('auth_token'),
        user: () => safeJSON(localStorage.getItem('user'), null),
        isLoggedIn: () => !!localStorage.getItem('auth_token'),
        logout: () => {
            localStorage.removeItem('auth_token');
            localStorage.removeItem('user');
            window.location.href = '/';
        },
        headers: () => {
            const t = localStorage.getItem('auth_token');
            return t ? { 'Authorization': 'Bearer ' + t } : {};
        }
    };

    // ---- Cart (localStorage) ----
    // Структура item: { id, name, price, quantity, image }
    const Cart = {
        items: () => safeJSON(localStorage.getItem('cart'), []),

        save(items) {
            localStorage.setItem('cart', JSON.stringify(items));
            this._notify();
        },

        add(product, quantity = 1) {
            const items = this.items();
            const id = Number(product.id);
            const existing = items.find((i) => i.id === id);
            const stock = Number(product.stock) || 0;
            const currentQty = existing ? existing.quantity : 0;

            if (stock > 0 && currentQty + quantity > stock) {
                toast(`Доступно только ${stock} шт.`, 'error');
                return false;
            }

            if (existing) {
                existing.quantity += quantity;
            } else {
                items.push({
                    id,
                    name: product.name,
                    price: Number(product.price) || 0,
                    quantity,
                    image: product.image || ''
                });
            }
            this.save(items);
            toast('Добавлено в корзину', 'success');
            return true;
        },

        setQty(id, quantity) {
            const items = this.items();
            const item = items.find((i) => i.id === Number(id));
            if (!item) return;
            if (quantity <= 0) {
                this.remove(id);
                return;
            }
            item.quantity = quantity;
            this.save(items);
        },

        remove(id) {
            const items = this.items().filter((i) => i.id !== Number(id));
            this.save(items);
        },

        clear() {
            this.save([]);
        },

        count() {
            return this.items().reduce((s, i) => s + i.quantity, 0);
        },

        total() {
            return this.items().reduce((s, i) => s + i.price * i.quantity, 0);
        },

        _notify() {
            updateCartBadge();
            renderCartDrawer();
            window.dispatchEvent(new CustomEvent('cart:changed'));
        }
    };

    // ---- Header ----
    function renderHeader() {
        const slot = document.querySelector('[data-header-slot]');
        if (!slot) return;
        const path = window.location.pathname;
        const isActive = (p) => (path === p ? 'active' : '');
        const user = Auth.user();
        const userBlock = user
            ? `<div class="user-menu">
                  <div class="user-avatar" id="userAvatar">${escapeHtml((user.name || user.username || user.email || 'U').charAt(0).toUpperCase())}</div>
                  <div class="dropdown-menu" id="dropdownMenu">
                      <a href="/profile">Личный кабинет</a>
                      ${user.role === 'admin' ? '<a href="/admin">Админ-панель</a>' : ''}
                      <a href="#" id="logoutLink">Выход</a>
                  </div>
               </div>`
            : `<a href="/login" class="btn btn-outline btn-sm">Войти</a>
               <a href="/register" class="btn btn-primary btn-sm">Регистрация</a>`;

        slot.innerHTML = `
            <header class="site-header">
                <div class="container header-inner">
                    <a href="/" class="logo">
                        <span class="logo-icon">🏡</span>
                        <span>ХозДача</span>
                    </a>
                    <nav class="header-nav">
                        <a href="/" class="${isActive('/')}">Главная</a>
                        <a href="/catalog" class="${isActive('/catalog')}">Каталог</a>
                    </nav>
                    <div class="header-actions">
                        <button class="cart-icon-btn" id="cartIconBtn" aria-label="Корзина">
                            🛒
                            <span class="cart-badge" id="cartBadge">0</span>
                        </button>
                        ${userBlock}
                    </div>
                </div>
            </header>
        `;

        // Привязки
        document.getElementById('cartIconBtn')?.addEventListener('click', openCart);
        document.getElementById('logoutLink')?.addEventListener('click', (e) => {
            e.preventDefault();
            Auth.logout();
        });
        const avatar = document.getElementById('userAvatar');
        if (avatar) {
            avatar.addEventListener('click', () => {
                document.getElementById('dropdownMenu').style.display =
                    document.getElementById('dropdownMenu').style.display === 'block' ? 'none' : 'block';
            });
            document.addEventListener('click', (e) => {
                if (!avatar.parentElement.contains(e.target)) {
                    const dd = document.getElementById('dropdownMenu');
                    if (dd) dd.style.display = 'none';
                }
            });
        }
        updateCartBadge();
    }

    function renderFooter() {
        const slot = document.querySelector('[data-footer-slot]');
        if (!slot) return;
        slot.innerHTML = `
            <footer class="site-footer">
                <div class="container">
                    <div class="footer-grid">
                        <div class="footer-col">
                            <h4>🏡 ХозДача</h4>
                            <p style="font-size:0.85rem;line-height:1.6">Хозяйственные товары, инструменты, садовый инвентарь и всё для дачи.</p>
                        </div>
                        <div class="footer-col">
                            <h4>Каталог</h4>
                            <a href="/catalog">Все товары</a>
                            <a href="/catalog">Инструменты</a>
                            <a href="/catalog">Сад и огород</a>
                            <a href="/catalog">Хозтовары</a>
                        </div>
                        <div class="footer-col">
                            <h4>Информация</h4>
                            <a href="#">Доставка</a>
                            <a href="#">Оплата</a>
                            <a href="#">Возврат</a>
                            <a href="#">Контакты</a>
                        </div>
                        <div class="footer-col">
                            <h4>Контакты</h4>
                            <a href="tel:+78000000000">8 (800) 000-00-00</a>
                            <a href="mailto:info@hozdacha.ru">info@hozdacha.ru</a>
                        </div>
                    </div>
                    <div class="footer-bottom">© ${new Date().getFullYear()} ХозДача — магазин хозяйственных товаров</div>
                </div>
            </footer>
        `;
    }

    // ---- Cart drawer ----
    function ensureCartDrawer() {
        if (document.getElementById('cartDrawer')) return;
        const overlay = document.createElement('div');
        overlay.className = 'cart-overlay';
        overlay.id = 'cartOverlay';
        overlay.addEventListener('click', closeCart);

        const drawer = document.createElement('aside');
        drawer.className = 'cart-drawer';
        drawer.id = 'cartDrawer';
        drawer.innerHTML = `
            <div class="cart-drawer-header">
                <h3>🛒 Корзина</h3>
                <button class="cart-drawer-close" id="cartDrawerClose">×</button>
            </div>
            <div class="cart-drawer-items" id="cartDrawerItems"></div>
            <div class="cart-drawer-footer">
                <div class="cart-total-row">
                    <span>Итого:</span>
                    <span id="cartDrawerTotal">0 ₽</span>
                </div>
                <button class="btn btn-accent" style="width:100%" id="cartDrawerCheckout">Оформить заказ</button>
            </div>
        `;
        document.body.appendChild(overlay);
        document.body.appendChild(drawer);
        document.getElementById('cartDrawerClose').addEventListener('click', closeCart);
        document.getElementById('cartDrawerCheckout').addEventListener('click', startCheckout);
    }

    function renderCartDrawer() {
        ensureCartDrawer();
        const items = Cart.items();
        const itemsEl = document.getElementById('cartDrawerItems');
        const totalEl = document.getElementById('cartDrawerTotal');
        if (!itemsEl) return;

        if (items.length === 0) {
            itemsEl.innerHTML = `
                <div class="cart-empty">
                    <div class="cart-empty-icon">🛒</div>
                    <p>Корзина пуста</p>
                    <a href="/catalog" class="btn btn-primary" style="margin-top:12px">К каталогу</a>
                </div>
            `;
        } else {
            itemsEl.innerHTML = items.map((it) => `
                <div class="cart-item-row" data-id="${it.id}">
                    <img class="cart-item-img" src="${escapeHtml(it.image) || PLACEHOLDER_IMG}" alt="" onerror="this.src='${PLACEHOLDER_IMG}'">
                    <div class="cart-item-info">
                        <div class="cart-item-name">${escapeHtml(it.name)}</div>
                        <div class="cart-item-price">${formatPrice(it.price)}</div>
                        <div class="cart-item-qty">
                            <button data-action="dec">−</button>
                            <span>${it.quantity}</span>
                            <button data-action="inc">+</button>
                        </div>
                    </div>
                    <button class="cart-item-remove" data-action="remove" aria-label="Удалить">🗑️</button>
                </div>
            `).join('');

            itemsEl.querySelectorAll('.cart-item-row').forEach((row) => {
                const id = Number(row.dataset.id);
                row.querySelector('[data-action="dec"]').onclick = () => {
                    const item = Cart.items().find((i) => i.id === id);
                    if (item) Cart.setQty(id, item.quantity - 1);
                };
                row.querySelector('[data-action="inc"]').onclick = () => {
                    const item = Cart.items().find((i) => i.id === id);
                    if (item) Cart.setQty(id, item.quantity + 1);
                };
                row.querySelector('[data-action="remove"]').onclick = () => Cart.remove(id);
            });
        }
        totalEl.textContent = formatPrice(Cart.total());
    }

    function openCart() {
        ensureCartDrawer();
        renderCartDrawer();
        document.getElementById('cartOverlay').classList.add('open');
        document.getElementById('cartDrawer').classList.add('open');
    }

    function closeCart() {
        document.getElementById('cartOverlay')?.classList.remove('open');
        document.getElementById('cartDrawer')?.classList.remove('open');
    }

    function updateCartBadge() {
        const badge = document.getElementById('cartBadge');
        if (badge) {
            const count = Cart.count();
            badge.textContent = count;
            badge.style.display = count === 0 ? 'none' : 'flex';
        }
    }

    // ---- Checkout ----
    function startCheckout() {
        if (Cart.items().length === 0) {
            toast('Корзина пуста', 'error');
            return;
        }
        if (!Auth.isLoggedIn()) {
            toast('Войдите, чтобы оформить заказ', 'info');
            setTimeout(() => {
                window.location.href = '/login?redirect=' + encodeURIComponent(window.location.pathname);
            }, 800);
            return;
        }
        closeCart();
        openCheckoutModal();
    }

    function openCheckoutModal() {
        const u = Auth.user() || {};
        let modal = document.getElementById('checkoutModal');
        if (!modal) {
            modal = document.createElement('div');
            modal.id = 'checkoutModal';
            modal.className = 'modal-overlay';
            modal.innerHTML = `
                <div class="modal-box" style="position:relative">
                    <button class="modal-close" id="checkoutClose">×</button>
                    <h2>Оформление заказа</h2>
                    <p style="color:var(--gray-600);font-size:0.9rem;margin-bottom:8px">
                        Товар будет забронирован для вас на 48 часов. Менеджер свяжется для уточнения деталей.
                    </p>
                    <form id="checkoutForm">
                        <input type="text" name="customer_name" placeholder="Ваше имя" required>
                        <input type="tel" name="phone" placeholder="Телефон (например +79001234567)" required>
                        <input type="text" name="address" placeholder="Адрес (необязательно)">
                        <textarea name="comment" placeholder="Комментарий"></textarea>
                        <button type="submit" class="btn btn-accent">Подтвердить заказ</button>
                    </form>
                </div>
            `;
            document.body.appendChild(modal);
            document.getElementById('checkoutClose').onclick = () => modal.classList.remove('open');
            modal.addEventListener('click', (e) => { if (e.target === modal) modal.classList.remove('open'); });
            document.getElementById('checkoutForm').addEventListener('submit', submitOrder);
        }
        // Префилл
        if (u.name) modal.querySelector('[name=customer_name]').value = u.name;
        if (u.phone) modal.querySelector('[name=phone]').value = u.phone;
        modal.classList.add('open');
    }

    async function submitOrder(e) {
        e.preventDefault();
        const form = e.target;
        const fd = new FormData(form);
        const customer_name = (fd.get('customer_name') || '').trim();
        const phone = (fd.get('phone') || '').trim();
        const address = (fd.get('address') || '').trim() || null;
        const comment = (fd.get('comment') || '').trim() || null;

        if (customer_name.length < 2) { toast('Введите имя', 'error'); return; }
        if (!/^[\d\s+\-()]{10,20}$/.test(phone)) { toast('Некорректный телефон', 'error'); return; }

        const items = Cart.items().map((i) => ({ product_id: i.id, quantity: i.quantity }));
        if (items.length === 0) { toast('Корзина пуста', 'error'); return; }

        const submitBtn = form.querySelector('button[type=submit]');
        submitBtn.disabled = true;
        submitBtn.textContent = 'Отправка...';

        try {
            const res = await fetch('/api/orders', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', ...Auth.headers() },
                body: JSON.stringify({ customer_name, phone, address, comment, items })
            });
            if (res.status === 401) {
                toast('Сессия истекла, войдите снова', 'error');
                Auth.logout();
                return;
            }
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Ошибка');
            toast('Заказ оформлен! Мы свяжемся с вами', 'success');
            Cart.clear();
            document.getElementById('checkoutModal').classList.remove('open');
            setTimeout(() => { window.location.href = '/profile'; }, 1200);
        } catch (err) {
            toast('Ошибка: ' + err.message, 'error');
        } finally {
            submitBtn.disabled = false;
            submitBtn.textContent = 'Подтвердить заказ';
        }
    }

    // ---- Init ----
    document.addEventListener('DOMContentLoaded', () => {
        renderHeader();
        renderFooter();
        ensureCartDrawer();
        updateCartBadge();
    });

    // Public API
    return { Cart, Auth, toast, escapeHtml, formatPrice, openCart, closeCart, startCheckout, PLACEHOLDER_IMG };
})();

window.App = App;
