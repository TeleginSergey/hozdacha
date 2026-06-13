// Страница /promotions — бесконечная лента: секции акций + товары внутри каждой.
(function() {
    'use strict';

    const FEED_PAGE_SIZE = 5;
    const PRODUCT_BATCH = 20;

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

    function promoId(promo) {
        return pickField(promo, 'ID', 'promotions_id_pk') || pickField(promo, 'id', 'id');
    }

    function renderSectionHeader(promo) {
        const title = escapeHtml(pickField(promo, 'Title', 'promotions_title') || 'Акция');
        const desc = pickField(promo, 'Description', 'promotions_description');
        const discount = parseFloat(pickField(promo, 'Discount', 'promotions_discount') || 0);
        const note = pickField(promo, 'ReservationNote', 'reservation_note') || '';
        const productCount = parseInt(pickField(promo, 'ProductCount', 'product_count') || 0);

        return ''
            + '<div class="promo-feed-section__head">'
            +   '<div class="promo-feed-section__title-row">'
            +     (discount > 0 ? '<span class="promo-feed-section__discount">−' + Math.round(discount) + '%</span>' : '')
            +     '<h2 class="promo-feed-section__title">' + title + '</h2>'
            +   '</div>'
            +   (desc ? '<p class="promo-feed-section__desc">' + escapeHtml(desc) + '</p>' : '')
            +   (note ? '<div class="promo-feed-section__note">' + escapeHtml(note) + '</div>' : '')
            +   (productCount > 0 ? '<div class="promo-feed-section__meta">' + productCount + ' '
                + (productCount === 1 ? 'товар' : (productCount < 5 ? 'товара' : 'товаров')) + '</div>' : '')
            + '</div>';
    }

    function renderProductCards(products) {
        if (!window.ProductCard) return '';
        return products.map(function(p) {
            return window.ProductCard.render(window.ProductCard.read(p));
        }).join('');
    }

    function createSection(promo) {
        const id = promoId(promo);
        const section = document.createElement('section');
        section.className = 'promo-feed-section';
        section.dataset.promoId = String(id);
        section.innerHTML = renderSectionHeader(promo)
            + '<div class="grid products-grid promo-feed-section__grid"></div>'
            + '<div class="scroll-sentinel promo-feed-section__sentinel" aria-hidden="true"></div>'
            + '<div class="scroll-loader promo-feed-section__loader" hidden>Загрузка товаров…</div>';
        return section;
    }

    function initSectionProducts(section) {
        const promoIdVal = section.dataset.promoId;
        const grid = section.querySelector('.promo-feed-section__grid');
        const sentinel = section.querySelector('.promo-feed-section__sentinel');
        const loader = section.querySelector('.promo-feed-section__loader');
        const state = { offset: 0, hasMore: true, loading: false };

        async function loadMore() {
            if (!state.hasMore || state.loading) return;
            state.loading = true;
            if (loader) loader.hidden = false;
            try {
                const resp = await fetch('/api/promotions/' + promoIdVal + '/products?limit=' + PRODUCT_BATCH + '&offset=' + state.offset);
                const data = await resp.json();
                const products = (data && data.products) || [];
                if (products.length > 0) {
                    grid.insertAdjacentHTML('beforeend', renderProductCards(products));
                    state.offset += products.length;
                }
                state.hasMore = !!(data && data.has_more);
                if (!state.hasMore && sentinel) {
                    sentinel.style.display = 'none';
                }
                if (state.offset === 0 && products.length === 0) {
                    grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Товаров по этой акции пока нет</p></div>';
                }
            } catch (e) {
                console.error('Failed to load promotion products:', e);
                if (state.offset === 0) {
                    grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Не удалось загрузить товары</p></div>';
                }
                state.hasMore = false;
            } finally {
                state.loading = false;
                if (loader) loader.hidden = true;
            }
        }

        if (window.InfiniteScroll) {
            window.InfiniteScroll.observe(sentinel, loadMore);
        }
        loadMore();
    }

    const feedState = {
        page: 1,
        hasMore: true,
        loading: false,
        disconnect: null,
        total: 0,
    };

    async function loadFeedPage() {
        if (!feedState.hasMore || feedState.loading) return;
        const feed = document.getElementById('promotionsFeed');
        const sentinel = document.getElementById('promotionsFeedSentinel');
        const loader = document.getElementById('promotionsFeedLoader');
        if (!feed) return;

        feedState.loading = true;
        if (loader) loader.hidden = false;

        try {
            const resp = await fetch('/api/promotions/feed?page=' + feedState.page + '&page_size=' + FEED_PAGE_SIZE);
            const data = await resp.json();
            const items = (data && data.items) || [];

            if (feedState.page === 1 && items.length === 0) {
                feed.innerHTML = '<div class="empty-state" style="grid-column:1/-1">'
                    + '<div class="empty-state__icon">🎁</div>'
                    + '<h3>Сейчас акций нет</h3>'
                    + '<p>Загляните позже — мы готовим новые предложения.</p>'
                    + '</div>';
                feedState.hasMore = false;
                if (sentinel) sentinel.style.display = 'none';
                updateCount(0, data && data.reservation_note);
                return;
            }

            items.forEach(function(promo) {
                const section = createSection(promo);
                feed.appendChild(section);
                initSectionProducts(section);
            });

            feedState.page += 1;
            feedState.hasMore = !!(data && data.has_more);
            feedState.total = (data && data.total) || feedState.total;
            updateCount(feedState.total, data && data.reservation_note);

            if (!feedState.hasMore && sentinel) {
                sentinel.style.display = 'none';
            }
        } catch (e) {
            console.error('Failed to load promotions feed:', e);
            if (feedState.page === 1) {
                feed.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Не удалось загрузить акции</p></div>';
            }
            feedState.hasMore = false;
        } finally {
            feedState.loading = false;
            if (loader) loader.hidden = true;
        }
    }

    function updateCount(total, globalNote) {
        const count = document.getElementById('promoCount');
        if (!count) return;
        if (total === 0) {
            count.textContent = 'Сейчас акций нет';
            return;
        }
        const word = function(n) {
            const m10 = n % 10, m100 = n % 100;
            if (m10 === 1 && m100 !== 11) return 'акция';
            if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return 'акции';
            return 'акций';
        };
        let text = 'Найдено: ' + total + ' ' + word(total);
        if (globalNote) {
            text += ' · ' + globalNote;
        }
        count.textContent = text;
    }

    document.addEventListener('DOMContentLoaded', function() {
        const sentinel = document.getElementById('promotionsFeedSentinel');
        if (window.InfiniteScroll && sentinel) {
            feedState.disconnect = window.InfiniteScroll.observe(sentinel, loadFeedPage);
        }
        loadFeedPage();
    });
})();
