// Страница /promotions — список всех действующих акций с фильтром и кликабельными CTA.
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

    function promotionHref(promo) {
        const productId = pickField(promo, 'FirstProductID', 'first_product_id');
        const categoryId = pickField(promo, 'FirstCategoryID', 'first_category_id');
        if (productId) return '/product/' + productId;
        if (categoryId) return '/catalog?category_id=' + categoryId;
        return '/catalog';
    }

    function promotionKind(promo) {
        const productCount = parseInt(pickField(promo, 'ProductCount', 'product_count') || 0);
        const categoryCount = parseInt(pickField(promo, 'CategoryCount', 'category_count') || 0);
        if (productCount > 0) return 'product';
        if (categoryCount > 0) return 'category';
        return 'all';
    }

    function renderCard(promo) {
        const title = escapeHtml(pickField(promo, 'Title', 'promotions_title') || 'Акция');
        const desc = pickField(promo, 'Description', 'promotions_description')
            ? escapeHtml(pickField(promo, 'Description', 'promotions_description'))
            : '';
        const discount = parseFloat(pickField(promo, 'Discount', 'promotions_discount') || 0);
        const productCount = parseInt(pickField(promo, 'ProductCount', 'product_count') || 0);
        const categoryCount = parseInt(pickField(promo, 'CategoryCount', 'category_count') || 0);
        const kind = promotionKind(promo);
        const href = promotionHref(promo);
        const ctaLabel = productCount > 0
            ? 'К товарам · ' + productCount
            : (categoryCount > 0 ? 'В категорию' : 'Смотреть товары');
        const kindLabel = productCount > 0 ? 'На товары' : 'На категории';

        return `
            <a class="promo-list-card" href="${href}" data-kind="${kind}">
                <div class="promo-list-card__top">
                    ${discount > 0 ? `<span class="promo-list-card__discount">−${Math.round(discount)}%</span>` : ''}
                    <span class="promo-list-card__tag">${kindLabel}</span>
                </div>
                <h3 class="promo-list-card__title">${title}</h3>
                ${desc ? `<p class="promo-list-card__desc">${desc}</p>` : '<p class="promo-list-card__desc promo-list-card__desc--empty">&nbsp;</p>'}
                <div class="promo-list-card__foot">
                    ${productCount > 0 ? `<span class="promo-list-card__meta">${productCount} ${productCount === 1 ? 'товар' : (productCount < 5 ? 'товара' : 'товаров')}</span>` : ''}
                    ${categoryCount > 0 ? `<span class="promo-list-card__meta">${categoryCount} ${categoryCount === 1 ? 'категория' : (categoryCount < 5 ? 'категории' : 'категорий')}</span>` : ''}
                    <span class="promo-list-card__cta">${ctaLabel}
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="16" height="16"><line x1="5" y1="12" x2="19" y2="12"></line><polyline points="12 5 19 12 12 19"></polyline></svg>
                    </span>
                </div>
            </a>`;
    }

    function applyFilter(promos, filter) {
        if (filter === 'all') return promos;
        return promos.filter(p => promotionKind(p) === filter);
    }

    function renderList(promos, filter) {
        const list = document.getElementById('promotionsList');
        if (!list) return;
        const filtered = applyFilter(promos, filter);
        if (filtered.length === 0) {
            list.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><div class="empty-state__icon">🎁</div><h3>Акций по выбранному фильтру нет</h3><p>Попробуйте выбрать «Все» или загляните позже.</p></div>';
            return;
        }
        list.innerHTML = filtered.map(renderCard).join('');
    }

    function setupFilter(promos) {
        const filter = document.getElementById('promoFilter');
        if (!filter) return;
        const hasProduct = promos.some(p => promotionKind(p) === 'product');
        const hasCategory = promos.some(p => promotionKind(p) === 'category');
        if (hasProduct || hasCategory) {
            filter.style.display = 'inline-flex';
        }
        const buttons = filter.querySelectorAll('.promo-filter__btn');
        buttons.forEach(btn => {
            btn.addEventListener('click', () => {
                buttons.forEach(b => b.classList.remove('active'));
                btn.classList.add('active');
                renderList(promos, btn.dataset.filter || 'all');
            });
        });
    }

    async function loadPromotions() {
        const list = document.getElementById('promotionsList');
        const count = document.getElementById('promoCount');
        try {
            const resp = await fetch('/api/promotions');
            const data = await resp.json();
            const promos = (data && data.promotions) || [];
            if (count) {
                if (promos.length === 0) {
                    count.textContent = 'Сейчас акций нет';
                } else {
                    const word = (n) => {
                        const m10 = n % 10, m100 = n % 100;
                        if (m10 === 1 && m100 !== 11) return 'акция';
                        if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return 'акции';
                        return 'акций';
                    };
                    count.textContent = promotionsRu(promos.length, word);
                }
            }
            if (promos.length === 0) {
                list.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><div class="empty-state__icon">🎁</div><h3>Сейчас акций нет</h3><p>Загляните позже — мы готовим новые предложения.</p></div>';
                return;
            }
            renderList(promos, 'all');
            setupFilter(promos);
        } catch (e) {
            console.error('Failed to load promotions:', e);
            if (list) list.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Не удалось загрузить акции</p></div>';
            if (count) count.textContent = '';
        }
    }

    function promotionsRu(n, wordFn) {
        return 'Найдено: ' + n + ' ' + wordFn(n);
    }

    document.addEventListener('DOMContentLoaded', loadPromotions);
})();
