// ХозДача — общие скрипты главной страницы.
// Использует общий модуль window.ProductCard (см. /static/js/product-card.js).
(function() {
    'use strict';

    function escapeHtml(text) {
        if (text === null || text === undefined) return '';
        const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' };
        return String(text).replace(/[&<>"']/g, m => map[m]);
    }

    function pickField(obj, camelName, snakeName) {
        if (!obj) return undefined;
        if (obj[camelName] !== undefined && obj[camelName] !== null) return obj[camelName];
        if (snakeName && obj[snakeName] !== undefined && obj[snakeName] !== null) return obj[snakeName];
        return undefined;
    }

    // Промо-карточки на главной: всегда ведут на страницу /promotions,
    // где уже есть полноценный feed товаров. Подсказка "сегодня/завтра" берётся
    // из DTO, которое отдаёт бэкенд (см. internal/handlers/promotions.go).
    function renderPromoCard(promo) {
        const title = escapeHtml(pickField(promo, 'Title', 'promotions_title') || 'Акция');
        const desc = pickField(promo, 'Description', 'promotions_description');
        const descHtml = desc ? '<p class="promo-card__desc">' + escapeHtml(desc) + '</p>' : '';
        const discount = parseFloat(pickField(promo, 'Discount', 'promotions_discount') || 0);
        const productCount = parseInt(pickField(promo, 'ProductCount', 'product_count') || 0);
        const note = pickField(promo, 'ReservationNote', 'reservation_note') || '';
        const cta = productCount > 0 ? 'Смотреть товары' : 'Смотреть акцию';

        let meta = '';
        if (productCount > 0) {
            const word = productCount === 1 ? 'товар'
                : (productCount < 5 ? 'товара' : 'товаров');
            meta = '<span class="promo-card__meta">' + productCount + ' ' + word + '</span>';
        }

        return ''
            + '<a class="promo-card" href="/promotions">'
            +   '<div class="promo-card__head">'
            +     (discount > 0 ? '<span class="promo-card__percent">−' + Math.round(discount) + '%</span>' : '')
            +     '<span class="promo-card__cta">' + cta
            +       '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="16" height="16">'
            +         '<line x1="5" y1="12" x2="19" y2="12"></line>'
            +         '<polyline points="12 5 19 12 12 19"></polyline>'
            +       '</svg>'
            +     '</span>'
            +   '</div>'
            +   '<div class="promo-card__title">' + title + '</div>'
            +   descHtml
            +   (note ? '<div class="promo-card__note">' + escapeHtml(note) + '</div>' : '')
            +   (meta ? '<div class="promo-card__foot">' + meta + '</div>' : '')
            + '</a>';
    }

    async function loadPromotions() {
        const grid = document.getElementById('promotionsGrid');
        if (!grid) return;
        try {
            const response = await fetch('/api/promotions');
            const data = await response.json();
            const promos = (data && data.promotions) || [];
            if (promos.length > 0) {
                grid.innerHTML = promos.map(renderPromoCard).join('');
            } else {
                grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1">'
                    + '<div class="empty-state__icon">🎁</div>'
                    + '<h3>Сейчас акций нет</h3>'
                    + '<p>Загляните позже — мы готовим новые предложения</p>'
                    + '<a href="/promotions" class="btn btn--outline btn--sm">Страница акций</a>'
                    + '</div>';
            }
        } catch (error) {
            console.error('Error loading promotions:', error);
            grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Не удалось загрузить акции</p></div>';
        }
    }

    async function loadProducts() {
        const grid = document.getElementById('productsGrid');
        if (!grid) return;
        try {
            const response = await fetch('/api/products?limit=10');
            const data = await response.json();
            const raw = (data && data.products) || [];
            if (raw.length > 0) {
                if (window.ProductCard) {
                    grid.innerHTML = raw.map(p => window.ProductCard.read(p)).map(p => window.ProductCard.render(p)).join('');
                } else {
                    grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Загрузка…</p></div>';
                }
            } else {
                grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1">'
                    + '<div class="empty-state__icon">📦</div>'
                    + '<h3>Товаров пока нет</h3>'
                    + '<p>Скоро здесь появятся новинки</p>'
                    + '</div>';
            }
        } catch (error) {
            console.error('Error loading products:', error);
            grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Не удалось загрузить товары</p></div>';
        }
    }

    document.addEventListener('DOMContentLoaded', () => {
        loadPromotions();
        loadProducts();
    });
})();
