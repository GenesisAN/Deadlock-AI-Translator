const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const script = fs.readFileSync(path.join(__dirname, '..', 'panorama', 'scripts', 'lc_chat.js'), 'utf8');

function chat(translate) {
	let now = 0;
	const timers = [];
	const sent = [];
	const calls = [];
	const rows = [];
	let tip;
	const messages = {
		IsValid: () => true,
		GetChildCount: () => rows.length,
		GetChild: index => rows[index]
	};
	const root = {
		IsValid: () => true,
		GetParent: () => null,
		FindChildTraverse(id) {
			if (id === 'ChatInput') return input;
			if (id === 'ChatMessages') return messages;
			if (id === 'LCTChatCommandTip') return tip || null;
			return null;
		}
	};
	const input = {
		text: '',
		IsValid: () => true,
		GetParent: () => root,
		SetPanelEvent(_event, callback) { this.submit = callback; }
	};
	const bridge = {
		getCfg: () => ({ enabled: true, outgoing: 'translation', outgoingTarget: 'en', uiLang: 'zh' }),
		translate(items, opts, callback) {
			calls.push({ items, opts, callback });
			translate(items, opts, callback, calls.length);
		}
	};
	const dollar = {
		GetContextPanel: () => root,
		Msg: () => {},
		Schedule(delay, callback) { timers.push({ at: now + delay, callback }); },
		DispatchEvent(event, panel) {
			if (event === 'CitadelChatInputSubmitted') sent.push(panel.text);
		},
		CreatePanel(_type, _parent, _id) {
			tip = { text: '', visible: false, hittest: false, style: {}, IsValid: () => true };
			return tip;
		}
	};
	vm.runInNewContext(script, { $: dollar, LCTBridge: bridge }, { filename: 'lc_chat.js' });

	function advance(seconds) {
		const end = now + seconds;
		while (true) {
			timers.sort((a, b) => a.at - b.at);
			if (!timers.length || timers[0].at > end) break;
			const task = timers.shift();
			now = task.at;
			task.callback();
		}
		now = end;
	}

	advance(0.5); // install the game's chat input hook
	return {
		input, sent, calls, advance,
		submit(text) { input.text = text; input.submit(); },
		receive(text, sender = 'Ally') {
			const source = {
				IsValid: () => true,
				FindChildrenWithClassTraverse(name) {
					if (name === 'SenderName') return [{ text: sender, IsValid: () => true }];
					if (name === 'ChannelName') return [{ text: 'Team', IsValid: () => true }];
					return [];
				}
			};
			const contents = { text, IsValid: () => true, GetChildCount: () => 0 };
			rows.push({
				IsValid: () => true,
				GetChildCount: () => 0,
				FindChildTraverse(id) {
					if (id === 'MessageSource') return source;
					if (id === 'MessageContents') return contents;
					return null;
				}
			});
		},
		get tip() { return tip; }
	};
}

test('three retries then only a local failure notice', () => {
	const ui = chat((_items, _opts, callback) => callback({ ok: false, code: 'PROVIDER_DOWN' }));
	ui.submit('我去守家');
	ui.advance(1.3);
	assert.equal(ui.calls.length, 4);
	assert.deepEqual(ui.sent, []);
	assert.equal(ui.input.text, '我去守家');
	assert.match(ui.tip.text, /已重试 3 次，消息未发送/);
	assert.equal(ui.tip.visible, true);
});

test('a later successful attempt sends the translation once', () => {
	const ui = chat((_items, _opts, callback, attempt) => {
		callback(attempt < 3 ? { ok: false, code: 'PROVIDER_DOWN' } :
			{ ok: true, items: [{ i: 0, t: 'I will defend base' }] });
	});
	ui.submit('我去守家');
	ui.advance(1);
	assert.equal(ui.calls.length, 3);
	assert.deepEqual(ui.sent, ['I will defend base']);
});

test('the next translation includes sent originals, not their translations', () => {
	const ui = chat((_items, _opts, callback, attempt) => {
		callback({ ok: true, items: [{ i: 0, t: attempt === 1 ? 'I will defend base' : 'Come mid' }] });
	});
	ui.submit('我去守家');
	ui.submit('来中路');
	assert.deepEqual(Array.from(ui.calls[0].opts.history), []);
	assert.deepEqual(Array.from(ui.calls[1].opts.history, item => ({ direction: item.direction, text: item.text })),
		[{ direction: 'outgoing', text: '我去守家' }]);
});

test('incoming originals reach later translations in chronological order', () => {
	const ui = chat((_items, _opts, callback) => callback({ ok: true, items: [{ i: 0, t: '译文' }] }));
	ui.receive('pig at mid');
	ui.advance(1);
	ui.receive('focus him');
	ui.advance(1);
	assert.deepEqual(Array.from(ui.calls[0].opts.history), []);
	assert.deepEqual(Array.from(ui.calls[1].opts.history, item => ({ direction: item.direction, text: item.text })),
		[{ direction: 'incoming', text: 'pig at mid' }]);
	ui.submit('我去中路');
	assert.deepEqual(Array.from(ui.calls[2].opts.history, item => item.text), ['pig at mid', 'focus him']);
});

test('history keeps ten messages and repeated text uses its new context', () => {
	const ui = chat((_items, _opts, callback) => callback({ ok: true, items: [{ i: 0, t: '译文' }] }));
	for (let i = 0; i < 12; i += 1) {
		ui.receive(`call ${i}`);
		ui.advance(1);
	}
	assert.equal(ui.calls.length, 12);
	assert.deepEqual(Array.from(ui.calls[11].opts.history, item => item.text),
		Array.from({ length: 10 }, (_, i) => `call ${i + 1}`));
	ui.receive('call 0');
	ui.advance(1);
	assert.equal(ui.calls.length, 13);
	assert.deepEqual(Array.from(ui.calls[12].opts.history, item => item.text),
		Array.from({ length: 10 }, (_, i) => `call ${i + 2}`));
});

test('silent timeouts and late callbacks never send the original', () => {
	const ui = chat(() => {});
	ui.submit('我去守家');
	ui.advance(202);
	assert.equal(ui.calls.length, 4);
	assert.deepEqual(ui.sent, []);
	ui.calls[0].callback({ ok: true, items: [{ i: 0, t: 'I will defend base' }] });
	assert.deepEqual(ui.sent, []);
	assert.equal(ui.input.text, '我去守家');
});

test('submitting the same text while pending does not send it raw', () => {
	const ui = chat(() => {});
	ui.submit('我去守家');
	ui.submit('我去守家');
	assert.equal(ui.calls.length, 1);
	assert.deepEqual(ui.sent, []);
	assert.equal(ui.input.text, '');
});

test('an exception and an empty result also use the retry path', () => {
	const ui = chat((_items, _opts, callback, attempt) => {
		if (attempt === 1) throw new Error('bridge failed');
		callback({ ok: true, items: [{ i: 0, t: '' }] });
	});
	ui.submit('我去守家');
	ui.advance(1.3);
	assert.equal(ui.calls.length, 4);
	assert.deepEqual(ui.sent, []);
	assert.match(ui.tip.text, /消息未发送/);
});

test('a second message keeps its own pending translation', () => {
	const ui = chat(() => {});
	ui.submit('我去守家');
	ui.submit('我去左路');
	assert.equal(ui.calls.length, 2);
	ui.calls[0].callback({ ok: false, code: 'PROVIDER_DOWN' });
	ui.calls[1].callback({ ok: true, items: [{ i: 0, t: 'I will go left' }] });
	ui.advance(0.5);
	assert.deepEqual(ui.sent, ['I will go left']);
	assert.equal(ui.calls.length, 3);
});
