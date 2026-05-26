// --- Table sorting ---
function sortCompletedTable(control, column, preserveDirection) {
	var th = control && control.closest ? control.closest('th[data-sort-key]') : control;
	if (!th) return;
	var shouldLoad = !preserveDirection;
	var applySort = function() {
		if (sortColumn !== column) {
			sortColumn = column;
			// Default ascending for text, descending for numbers
			sortAsc = ['name', 'provider', 'model', 'project', 'id'].indexOf(column) >= 0;
		} else if (!preserveDirection) {
			sortAsc = !sortAsc;
		}
		updateCompletedSortIndicators();
		if (shouldLoad) sortCurrentCompletedRows(column);
	};
	if (typeof withDashboardScrollStability === 'function') {
		withDashboardScrollStability(applySort, {completedRegion: true});
	} else {
		applySort();
	}
	if (shouldLoad) {
		loadCompletedSessions(0);
	}
}

function sortCurrentCompletedRows(column) {
	var tbody = document.getElementById('completed-sessions');
	if (!tbody) return;
	var rows = Array.from(tbody.querySelectorAll('tr[data-sort-ended]:not([data-parent])'));
	var numericCols = ['tokens', 'turns', 'tools', 'duration', 'ended'];
	var isNumeric = numericCols.indexOf(column) >= 0;
	var paginationRow = tbody.querySelector('tr[data-pagination-row]');
	var subagentRows = Array.from(tbody.querySelectorAll('tr[data-parent]'));
	var subagentsByParent = {};
	subagentRows.forEach(function(row) {
		var pid = row.getAttribute('data-parent');
		if (!subagentsByParent[pid]) subagentsByParent[pid] = [];
		subagentsByParent[pid].push(row);
		row.remove();
	});
	rows.sort(function(a, b) {
		var aVal = a.getAttribute('data-sort-' + column) || '';
		var bVal = b.getAttribute('data-sort-' + column) || '';
		if (isNumeric) {
			var diff = (parseFloat(aVal) || 0) - (parseFloat(bVal) || 0);
			return sortAsc ? diff : -diff;
		}
		var cmp = aVal.localeCompare(bVal, undefined, {sensitivity: 'base'});
		return sortAsc ? cmp : -cmp;
	});
	rows.forEach(function(row) {
		if (paginationRow) tbody.insertBefore(row, paginationRow);
		else tbody.appendChild(row);
		var parentID = row.id.replace('session-row-', '');
		(subagentsByParent[parentID] || []).forEach(function(subRow) {
			row.after(subRow);
			row = subRow;
		});
	});
}

function updateCompletedSortIndicators() {
	document.querySelectorAll('#completed-table th[data-sort-key]').forEach(function(header) {
		header.setAttribute('aria-sort', 'none');
		var headerArrow = header.querySelector('.sort-arrow');
		if (headerArrow) {
			headerArrow.classList.remove('active');
			headerArrow.textContent = '▼';
		}
	});
	var th = document.querySelector('#completed-table th[data-sort-key="' + sortColumn + '"]');
	if (!th) return;
	th.setAttribute('aria-sort', sortAsc ? 'ascending' : 'descending');
	var arrow = th.querySelector('.sort-arrow');
	if (arrow) {
		arrow.classList.add('active');
		arrow.textContent = sortAsc ? '▲' : '▼';
	}
}
