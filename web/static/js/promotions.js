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
            populateCategoryFilter();
        }
        if (data && data.promotions) {
            state.promotions = data.promotions;
            renderPromoChips();
        }
    }

    function populateCategoryFilter() {
        const select = document.getElementById('promoCategoryFilter');
        if (!select) return;
        const current = state.categoryId;
        let html = '<option value="">Все категории</option>';
        state.categories.forEach(function(cat) {
            const sel = String(cat.id) === current ? ' selected' : '';
            html += '<option value="' + cat.id + '"' + sel + '>' + escapeHtml(cat.name) + '</option>';
        });
        select.innerHTML = html;
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
        const categorySelect = document.getElementById('promoCategoryFilter');
        state.query = (searchInput && searchInput.value || '').trim();
        state.categoryId = (categorySelect && categorySelect.value) || '';
        resetList();
        loadProducts(false);
    }

    function setupFilters() {
        const searchBtn = document.getElementById('promoSearchBtn');
        const searchInput = document.getElementById('promoSearchInput');
        const categorySelect = document.getElementById('promoCategoryFilter');
        const kindFilter = document.getElementById('promoKindFilter');
        const resetBtn = document.getElementById('promoResetBtn');

        if (searchBtn) searchBtn.addEventListener('click', applyFilters);
        if (searchInput) {
            searchInput.addEventListener('keydown', function(e) {
                if (e.key === 'Enter') applyFilters();
            });
        }
        if (categorySelect) {
            categorySelect.addEventListener('change', applyFilters);
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
                if (categorySelect) categorySelect.value = '';
                state.query = '';
                state.categoryId = '';
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
