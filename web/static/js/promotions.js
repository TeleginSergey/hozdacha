// Страница /promotions — каталог акционных товаров с фильтрами.
(function() {
    'use strict';

    const PAGE_SIZE = 24;

    const state = {
        offset: 0,
        hasMore: true,
        loading: false,
        query: '',
        categoryId: '',
        kind: 'all',
        categories: [],
        promotions: [],
        reservationNote: '',
    };

    function escapeHtml(text) {
        if (text === null || text === undefined) return '';
        const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' };
        return String(text).replace(/[&<>"']/g, m => map[m]);
    }

    function renderProductCards(products) {
        if (!window.ProductCard) return '';
        return products.map(function(p) {
            return window.ProductCard.render(window.ProductCard.read(p), { showPromoLabel: true });
        }).join('');
    }

    function buildApiUrl() {
        const params = new URLSearchParams();
        params.set('limit', String(PAGE_SIZE));
        params.set('offset', String(state.offset));
        if (state.query) params.set('q', state.query);
        if (state.categoryId) params.set('category_id', state.categoryId);
        if (state.kind && state.kind !== 'all') params.set('kind', state.kind);
        return '/api/promotions/products?' + params.toString();
    }

    function updateMeta(data) {
        const countEl = document.getElementById('promoCount');
        const noteEl = document.getElementById('promoReservationNote');
        const total = (data && data.total) || 0;

        if (countEl) {
            if (total === 0) {
                countEl.textContent = 'Акционных товаров не найдено';
            } else {
                const word = total === 1 ? 'товар' : (total < 5 ? 'товара' : 'товаров');
                countEl.textContent = 'Найдено: ' + total + ' ' + word + ' со скидкой';
            }
        }

        state.reservationNote = (data && data.reservation_note) || '';
        if (noteEl) {
            noteEl.textContent = state.reservationNote;
            noteEl.hidden = !state.reservationNote;
        }

        if (data && data.categories) {
            state.categories = data.categories;
            renderCategoryChips();
        }
        if (data && data.promotions) {
            state.promotions = data.promotions;
            renderPromoChips();
        }
    }

    function renderCategoryChips() {
        const wrap = document.getElementById('promoCategoryChips');
        if (!wrap) return;
        const current = state.categoryId;

        // Базовая кнопка «Все категории».
        const allBtn = '<button class="promo-cat-chip' + (current === '' ? ' active' : '') + '"'
            + ' type="button" data-category-id="">Все категории</button>';

        // Каждая категория с подсказкой «−30% · Скидка саженцы · Категория» из привязанных акций.
        const catButtons = state.categories.map(function(cat) {
            const sel = String(cat.id) === current ? ' active' : '';
            const promos = Array.isArray(cat.promos) ? cat.promos : [];
            let promoLine = '';
            if (promos.length > 0) {
                const top = promos[0];
                const kindLabel = top.kind === 'category' ? 'Категория' : (top.kind === 'mixed' ? 'Смешанная' : 'Товары');
                promoLine = '<span class="promo-cat-chip__promo">'
                    + '<span class="promo-cat-chip__discount">−' + Math.round(top.discount || 0) + '%</span>'
                    + '<span class="promo-cat-chip__title">' + escapeHtml(top.title || 'Акция') + '</span>'
                    + '<span class="promo-cat-chip__tag">' + kindLabel + '</span>'
                    + '</span>';
            }
            return '<button class="promo-cat-chip' + sel + '" type="button" data-category-id="' + cat.id + '">'
                + '<span class="promo-cat-chip__name">' + escapeHtml(cat.name) + '</span>'
                + promoLine
                + '</button>';
        }).join('');

        wrap.innerHTML = allBtn + catButtons;
    }

    function renderPromoChips() {
        const wrap = document.getElementById('promoChips');
        if (!wrap) return;
        if (!state.promotions.length) {
            wrap.innerHTML = '';
            wrap.hidden = true;
            return;
        }
        wrap.hidden = false;
        wrap.innerHTML = state.promotions.map(function(p) {
            const kindLabel = p.kind === 'category' ? 'Категория' : (p.kind === 'mixed' ? 'Смешанная' : 'Товары');
            return '<div class="promo-chip">'
                + '<span class="promo-chip__discount">−' + Math.round(p.discount || 0) + '%</span>'
                + '<span class="promo-chip__title">' + escapeHtml(p.title || 'Акция') + '</span>'
                + '<span class="promo-chip__tag">' + kindLabel + '</span>'
                + '</div>';
        }).join('');
    }

    function resetList() {
        state.offset = 0;
        state.hasMore = true;
        const sentinel = document.getElementById('promoProductsSentinel');
        if (sentinel) sentinel.style.display = 'block';
    }

    async function loadProducts(append) {
        if (state.loading) return;
        if (append && !state.hasMore) return;

        const grid = document.getElementById('promoProductsGrid');
        const loader = document.getElementById('promoProductsLoader');
        const sentinel = document.getElementById('promoProductsSentinel');
        if (!grid) return;

        state.loading = true;
        if (!append) {
            grid.innerHTML = '<div class="promo-page__loading" style="grid-column:1/-1">Загрузка…</div>';
        } else if (loader) {
            loader.hidden = false;
        }

        try {
            const resp = await fetch(buildApiUrl());
            const data = await resp.json();
            const products = (data && data.products) || [];

            if (!append) {
                updateMeta(data);
            }

            if (products.length > 0) {
                const html = renderProductCards(products);
                if (append) {
                    grid.insertAdjacentHTML('beforeend', html);
                } else {
                    grid.innerHTML = html;
                }
                state.offset += products.length;
                state.hasMore = !!(data && data.has_more);
            } else if (!append) {
                grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1">'
                    + '<div class="empty-state__icon">🔍</div>'
                    + '<h3>Ничего не найдено</h3>'
                    + '<p>Попробуйте другой запрос или сбросьте фильтры.</p>'
                    + '</div>';
                state.hasMore = false;
            } else {
                state.hasMore = false;
            }

            if (!state.hasMore && sentinel) sentinel.style.display = 'none';
        } catch (e) {
            console.error('Failed to load promotional products:', e);
            if (!append) {
                grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Не удалось загрузить товары</p></div>';
            }
            state.hasMore = false;
        } finally {
            state.loading = false;
            if (loader) loader.hidden = true;
        }
    }

    function applyFilters() {
        const searchInput = document.getElementById('promoSearchInput');
        state.query = (searchInput && searchInput.value || '').trim();
        resetList();
        loadProducts(false);
    }

    function setCategory(catId) {
        state.categoryId = catId != null ? String(catId) : '';
        // Подсветка активной кнопки.
        const wrap = document.getElementById('promoCategoryChips');
        if (wrap) {
            wrap.querySelectorAll('.promo-cat-chip').forEach(function(btn) {
                btn.classList.toggle('active', (btn.dataset.categoryId || '') === state.categoryId);
            });
        }
        resetList();
        loadProducts(false);
    }

    function setupFilters() {
        const searchBtn = document.getElementById('promoSearchBtn');
        const searchInput = document.getElementById('promoSearchInput');
        const categoryChips = document.getElementById('promoCategoryChips');
        const kindFilter = document.getElementById('promoKindFilter');
        const resetBtn = document.getElementById('promoResetBtn');

        if (searchBtn) searchBtn.addEventListener('click', applyFilters);
        if (searchInput) {
            searchInput.addEventListener('keydown', function(e) {
                if (e.key === 'Enter') applyFilters();
            });
        }
        if (categoryChips) {
            categoryChips.addEventListener('click', function(e) {
                const btn = e.target.closest('.promo-cat-chip');
                if (!btn) return;
                setCategory(btn.dataset.categoryId || '');
            });
        }
        if (kindFilter) {
            kindFilter.querySelectorAll('.promo-filter__btn').forEach(function(btn) {
                btn.addEventListener('click', function() {
                    kindFilter.querySelectorAll('.promo-filter__btn').forEach(function(b) {
                        b.classList.remove('active');
                    });
                    btn.classList.add('active');
                    state.kind = btn.dataset.kind || 'all';
                    resetList();
                    loadProducts(false);
                });
            });
        }
        if (resetBtn) {
            resetBtn.addEventListener('click', function() {
                if (searchInput) searchInput.value = '';
                state.query = '';
                state.categoryId = '';
                setCategory('');
                state.kind = 'all';
                if (kindFilter) {
                    kindFilter.querySelectorAll('.promo-filter__btn').forEach(function(b) {
                        b.classList.toggle('active', (b.dataset.kind || 'all') === 'all');
                    });
                }
                resetList();
                loadProducts(false);
            });
        }
    }

    document.addEventListener('DOMContentLoaded', function() {
        setupFilters();
        const sentinel = document.getElementById('promoProductsSentinel');
        if (window.InfiniteScroll && sentinel) {
            window.InfiniteScroll.observe(sentinel, function() {
                return loadProducts(true);
            });
        }
        loadProducts(false);
    });
})();
