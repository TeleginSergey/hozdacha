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
        catFilter: '',
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
            renderCategoryUI();
        }
        if (data && data.promotions) {
            state.promotions = data.promotions;
            renderPromoChips();
        }
    }

    // Возвращает понятное имя выбранной категории для подписи триггера.
    function categoryNameById(catId) {
        const found = state.categories.find(function(c) { return String(c.id) === String(catId); });
        return found ? found.name : '';
    }

    // Обновляет подпись на кнопке-триггере дропдауна.
    function updateCategoryLabel() {
        const label = document.getElementById('promoCatLabel');
        if (!label) return;
        if (!state.categoryId) {
            label.textContent = 'Все категории';
            label.classList.remove('is-selected');
        } else {
            label.textContent = categoryNameById(state.categoryId) || 'Категория';
            label.classList.add('is-selected');
        }
    }

    // Рендерит список категорий внутри выпадающей панели с учётом строки поиска.
    function renderCategoryOptions() {
        const list = document.getElementById('promoCatList');
        if (!list) return;
        const current = state.categoryId;
        const filter = state.catFilter.trim().toLowerCase();

        function optionRow(catId, name, promos, isAll) {
            const sel = String(catId) === String(current) ? ' active' : '';
            let badge = '';
            const arr = Array.isArray(promos) ? promos : [];
            if (arr.length > 0) {
                const top = arr[0];
                badge = '<span class="promo-catopt__badge">−' + Math.round(top.discount || 0) + '%</span>';
            }
            const check = '<svg class="promo-catopt__check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" width="15" height="15" aria-hidden="true"><polyline points="20 6 9 17 4 12"></polyline></svg>';
            return '<button class="promo-catopt' + sel + '" type="button" role="option" data-category-id="' + (catId === '' ? '' : catId) + '"'
                + (sel ? ' aria-selected="true"' : '') + '>'
                + '<span class="promo-catopt__name">' + escapeHtml(name) + '</span>'
                + badge
                + check
                + '</button>';
        }

        let html = '';
        // «Все категории» показываем всегда (и без фильтра, и если совпадает с запросом).
        if (!filter || 'все категории'.indexOf(filter) !== -1) {
            html += optionRow('', 'Все категории', null, true);
        }

        const matched = state.categories.filter(function(cat) {
            if (!filter) return true;
            return String(cat.name || '').toLowerCase().indexOf(filter) !== -1;
        });

        html += matched.map(function(cat) {
            return optionRow(cat.id, cat.name, cat.promos, false);
        }).join('');

        if (!matched.length && filter && 'все категории'.indexOf(filter) === -1) {
            html += '<div class="promo-catopt__empty">Категория не найдена</div>';
        }

        list.innerHTML = html;
    }

    // Полное обновление UI категорий (подпись + список).
    function renderCategoryUI() {
        updateCategoryLabel();
        renderCategoryOptions();
    }

    function openCatPanel() {
        const panel = document.getElementById('promoCatPanel');
        const trigger = document.getElementById('promoCatTrigger');
        const select = document.getElementById('promoCatSelect');
        if (!panel || !trigger) return;
        panel.hidden = false;
        trigger.setAttribute('aria-expanded', 'true');
        if (select) select.classList.add('is-open');
        const search = document.getElementById('promoCatSearch');
        if (search) { search.value = state.catFilter; setTimeout(function() { search.focus(); }, 0); }
    }

    function closeCatPanel() {
        const panel = document.getElementById('promoCatPanel');
        const trigger = document.getElementById('promoCatTrigger');
        const select = document.getElementById('promoCatSelect');
        if (!panel || !trigger) return;
        panel.hidden = true;
        trigger.setAttribute('aria-expanded', 'false');
        if (select) select.classList.remove('is-open');
    }

    function isCatPanelOpen() {
        const panel = document.getElementById('promoCatPanel');
        return panel && !panel.hidden;
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

    function setCategory(catId, opts) {
        state.categoryId = catId != null ? String(catId) : '';
        renderCategoryUI();
        if (!(opts && opts.keepOpen)) closeCatPanel();
        resetList();
        loadProducts(false);
    }

    function setupFilters() {
        const searchBtn = document.getElementById('promoSearchBtn');
        const searchInput = document.getElementById('promoSearchInput');
        const catSelect = document.getElementById('promoCatSelect');
        const catTrigger = document.getElementById('promoCatTrigger');
        const catList = document.getElementById('promoCatList');
        const catSearch = document.getElementById('promoCatSearch');
        const kindFilter = document.getElementById('promoKindFilter');
        const resetBtn = document.getElementById('promoResetBtn');

        if (searchBtn) searchBtn.addEventListener('click', applyFilters);
        if (searchInput) {
            searchInput.addEventListener('keydown', function(e) {
                if (e.key === 'Enter') applyFilters();
            });
        }

        // Выпадающий список категорий: открытие/закрытие.
        if (catTrigger) {
            catTrigger.addEventListener('click', function(e) {
                e.stopPropagation();
                if (isCatPanelOpen()) closeCatPanel();
                else openCatPanel();
            });
        }
        // Выбор категории из списка.
        if (catList) {
            catList.addEventListener('click', function(e) {
                const opt = e.target.closest('.promo-catopt');
                if (!opt) return;
                setCategory(opt.dataset.categoryId || '');
            });
        }
        // Живой поиск внутри дропдауна.
        if (catSearch) {
            catSearch.addEventListener('input', function() {
                state.catFilter = catSearch.value || '';
                renderCategoryOptions();
            });
            catSearch.addEventListener('keydown', function(e) {
                if (e.key === 'Escape') { closeCatPanel(); catTrigger && catTrigger.focus(); }
            });
        }
        // Закрытие по клику вне и по Escape.
        document.addEventListener('click', function(e) {
            if (!isCatPanelOpen()) return;
            if (catSelect && !catSelect.contains(e.target)) closeCatPanel();
        });
        document.addEventListener('keydown', function(e) {
            if (e.key === 'Escape' && isCatPanelOpen()) closeCatPanel();
        });
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
                if (catSearch) catSearch.value = '';
                state.query = '';
                state.categoryId = '';
                state.catFilter = '';
                state.kind = 'all';
                if (kindFilter) {
                    kindFilter.querySelectorAll('.promo-filter__btn').forEach(function(b) {
                        b.classList.toggle('active', (b.dataset.kind || 'all') === 'all');
                    });
                }
                renderCategoryUI();
                closeCatPanel();
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
