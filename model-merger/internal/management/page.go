package management

import (
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func pageResponse() pluginapi.ManagementResponse {
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}, "Cache-Control": []string{"no-store"}},
		Body:       []byte(pageHTML),
	}
}

// pageHTML is static: it holds no data and no key. It asks for the management
// key once per tab (sessionStorage) and drops a rejected key right away, so one
// wrong key costs one failed attempt instead of feeding the host's IP ban.
const pageHTML = `<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Model Merger</title>
<style>
:root{color-scheme:dark}*{box-sizing:border-box}body{margin:0;padding:24px;background:#101723;color:#e7eaf1;font:14px system-ui,-apple-system,Segoe UI,sans-serif}
.page{max-width:1180px;margin:0 auto}.bar{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:16px}
h1{font-size:19px;margin:0}h2{font-size:16px;margin:0 0 12px}.count{background:#0f4777;color:#9bd0ff;border-radius:999px;padding:3px 9px;font-weight:800;font-size:12px;margin-left:8px}
.btn{border:1px solid #4776a8;background:#213a58;color:#dbeafe;border-radius:10px;padding:8px 13px;font-weight:800;cursor:pointer;font:inherit;font-weight:800}.btn.primary{background:#2f94ff;color:#fff}.btn.danger{border-color:#8e3b4b;background:#3a1d26;color:#ffb1b7}.btn.small{padding:4px 9px;font-size:12px}.btn:disabled{opacity:.4;cursor:not-allowed}
.card{background:#151b2a;border:1px solid #293346;border-radius:10px;padding:16px;margin-bottom:12px}.row{display:flex;gap:10px;align-items:center;flex-wrap:wrap}.grow{flex:1}
.alias{font-family:ui-monospace,Consolas,monospace;font-size:16px;font-weight:900}.chain{color:#aeb7c6;margin-top:6px;font-family:ui-monospace,Consolas,monospace;overflow-wrap:anywhere}.muted{color:#8d95a3;font-size:12px}
.badge{border-radius:999px;padding:3px 9px;font-size:12px;font-weight:800}.on{background:#173d22;color:#8ef0a3}.off{background:#3a2a1a;color:#f6c177}.err{background:#381b21;color:#ffd5d5}
label{font-weight:700}.field{margin:12px 0}.field>label{display:block;margin-bottom:6px}input[type=text],input[type=number],textarea,select{width:100%;border:1px solid #354156;background:#0c1220;color:#f4f7fb;border-radius:8px;padding:8px;font:inherit}textarea{min-height:70px;font-family:ui-monospace,Consolas,monospace}
.targets .t{display:grid;grid-template-columns:28px 1fr 120px auto;gap:8px;align-items:center;margin-bottom:6px}.pos{color:#8d95a3;font-weight:800;text-align:center}
.switch{display:flex;gap:10px;align-items:flex-start;cursor:pointer}.switch input{width:38px;height:20px;accent-color:#2f94ff;margin:2px 0 0}.hint{color:#9da7b8;font-size:12px;margin-top:3px;font-weight:400}
.msg{padding:10px 12px;border-radius:8px;margin:10px 0}.msg.bad{background:#381b21;color:#ffd5d5;border:1px solid #7f3131}.msg.good{background:#142319;color:#b9f5c6;border:1px solid #2d5d41}.msg.warn{background:#33290f;color:#f6d38a;border:1px solid #6b5520}
.keyform{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:16px 0;padding:12px;border:1px solid #4776a8;border-radius:9px;background:#151b2a}.keyform.hidden,.hidden{display:none}.keyform label{width:100%}.keyform input{flex:1;min-width:220px;width:auto;border:1px solid #354156;background:#0c1220;color:#f4f7fb;border-radius:8px;padding:8px}.keyform span{width:100%;color:#ff9faa}
.cols{display:grid;grid-template-columns:1fr 1fr 1fr;gap:12px}@media(max-width:760px){body{padding:14px}.cols{grid-template-columns:1fr}.targets .t{grid-template-columns:22px 1fr 80px}.targets .t .acts{grid-column:2/4}}
</style></head><body><main class="page">
<form id="key-form" class="keyform hidden"><label for="key-input">CLIProxyAPI Management-Key</label><input id="key-input" type="password" autocomplete="off"><button class="btn primary" type="submit">Key verwenden</button><span id="key-error"></span></form>
<div class="bar"><div class="row"><h1>Model Merger</h1><span id="count" class="count">0</span></div><div class="row"><button id="reload" class="btn" type="button">Neu laden</button><button id="new" class="btn primary" type="button">Neues virtuelles Modell</button></div></div>
<div id="messages"></div>
<section id="editor" class="card hidden"><h2 id="editor-title">Virtuelles Modell</h2>
<div class="cols"><div class="field"><label for="f-alias">Alias</label><input id="f-alias" type="text" placeholder="mixed-image-gen" autocomplete="off"></div>
<div class="field"><label for="f-type">Modelltyp</label><select id="f-type"><option value="openai-image">openai-image (/v1/images/generations)</option><option value="chat" disabled>chat (später)</option></select></div>
<div class="field"><label for="f-cooldown">Cooldown (Sekunden)</label><input id="f-cooldown" type="number" min="0" step="1" value="0"><div class="hint">Ein ausgefallenes Target wird so lange übersprungen. 0 = aus.</div></div></div>
<div class="field"><label>Targets (Reihenfolge = Fallback-Kette)</label><div class="hint" style="margin-bottom:8px">Nur vorwärts: Scheitert ein Fallback, gibt es keinen Rücksprung. Timeout in Sekunden, leer = kein eigenes Limit.</div><div id="f-targets" class="targets"></div><button id="add-target" class="btn small" type="button">+ Target</button></div>
<div class="cols"><div class="field"><label for="f-fallback">Fallback bei Status</label><input id="f-fallback" type="text" placeholder="408, 429, 500, 502, 503, 504"><div class="hint">Transportfehler und Timeouts wechseln immer zum nächsten Target.</div></div>
<div class="field"><label for="f-nofallback">Kein Fallback bei Status</label><input id="f-nofallback" type="text" placeholder="z. B. 401"><div class="hint">Hat Vorrang vor „Fallback bei Status“.</div></div>
<div class="field"><label class="switch"><input id="f-enabled" type="checkbox" checked><span>Aktiv</span></label></div></div>
<div class="field"><label class="switch"><input id="f-moderation" type="checkbox"><span>Bei Inhaltsfilter zum nächsten Modell wechseln<div class="hint">Das nächste Modell wendet seine eigenen Regeln an.</div></span></label></div>
<div class="field"><label for="f-patterns">Inhaltsfilter-Muster (eins pro Zeile)</label><textarea id="f-patterns"></textarea><div class="hint">Teilstrings in error.message (ohne Groß-/Kleinschreibung). Zusätzlich erkannt: error.code „moderation_blocked“ und error.type „image_generation_user_error“.</div></div>
<div class="row"><button id="save" class="btn primary" type="button">Speichern</button><button id="cancel" class="btn" type="button">Abbrechen</button></div></section>
<datalist id="model-ids"></datalist><div id="list"><p class="muted">Lade virtuelle Modelle…</p></div></main>
<script>
(function(){
var KEY="nopro200-model-merger-management-key",API="/v0/management/plugins/model-merger/models.json",state={models:[],defaults:{},editing:null},onKey=null;
function $(id){return document.getElementById(id)}
function esc(v){return String(v==null?"":v).replace(/[&<>"']/g,function(c){return{"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c]})}
function getKey(){try{return sessionStorage.getItem(KEY)||""}catch(e){return ""}}
function setKey(v){try{if(v){sessionStorage.setItem(KEY,v)}else{sessionStorage.removeItem(KEY)}}catch(e){}}
function askKey(msg){var f=$("key-form");$("key-error").textContent=msg||"";if(f.classList.contains("hidden")){f.classList.remove("hidden");$("key-input").focus()}}
$("key-form").addEventListener("submit",function(ev){ev.preventDefault();var v=$("key-input").value.trim();if(!v)return;setKey(v);$("key-input").value="";$("key-error").textContent="";$("key-form").classList.add("hidden");if(onKey)onKey()});
async function api(method,url,body){var key=getKey();if(!key){askKey("");throw new Error("Bitte den CLIProxyAPI Management-Key eingeben.")}
var opts={method:method,cache:"no-store",credentials:"same-origin",headers:{"Authorization":"Bearer "+key,"Accept":"application/json"}};if(body!==undefined){opts.headers["Content-Type"]="application/json";opts.body=JSON.stringify(body)}
var res=await fetch(url,opts);if(res.status===401){setKey("");askKey("Der Management-Key wurde abgelehnt.");throw new Error("Der Management-Key wurde abgelehnt.")}
var text=await res.text(),data={};try{data=text?JSON.parse(text):{}}catch(e){data={error:text}}if(!res.ok){var err=new Error(data.error||("HTTP "+res.status));err.problems=data.problems;throw err}return data}
function message(kind,text,list){var box=document.createElement("div");box.className="msg "+kind;box.textContent=text;if(list&&list.length){var ul=document.createElement("ul");list.forEach(function(p){var li=document.createElement("li");li.textContent=p;ul.appendChild(li)});box.appendChild(ul)}$("messages").appendChild(box)}
function clearMessages(){$("messages").innerHTML=""}
function statuses(text){return String(text||"").split(/[\s,;]+/).filter(Boolean).map(Number)}
function render(){$("count").textContent=state.models.length;var list=$("list");if(!state.models.length){list.innerHTML='<p class="muted">Noch keine virtuellen Modelle.</p>';return}
list.innerHTML=state.models.map(function(m,i){var chain=(m.targets||[]).map(function(t){return esc(t.model)+(t.timeout?' <span class="muted">('+t.timeout+' s)</span>':"")}).join(" &rarr; ");
var st=m.enabled?'<span class="badge on">Aktiv</span>':'<span class="badge off">Inaktiv</span>';if(m.status==="error")st+=' <span class="badge err" title="'+esc(m.status_message)+'">Fehler</span>';
return '<div class="card"><div class="row"><span class="alias grow">'+esc(m.alias)+'</span>'+st+'<button class="btn small" data-act="edit" data-i="'+i+'">Bearbeiten</button><button class="btn small danger" data-act="del" data-i="'+i+'">Löschen</button></div><div class="chain">'+chain+'</div>'+
'<div class="row" style="margin-top:10px"><label class="switch"><input type="checkbox" data-act="enabled" data-i="'+i+'"'+(m.enabled?" checked":"")+'><span>Aktiv</span></label><label class="switch"><input type="checkbox" data-act="moderation" data-i="'+i+'"'+(m.fallback_on_moderation?" checked":"")+'><span>Bei Inhaltsfilter zum nächsten Modell wechseln<div class="hint">Das nächste Modell wendet seine eigenen Regeln an.</div></span></label></div>'+
'<div class="muted" style="margin-top:6px">Fallback bei: '+esc((m.fallback_on_status||[]).join(", ")||"–")+' · kein Fallback bei: '+esc((m.no_fallback_on_status||[]).join(", ")||"–")+' · Cooldown: '+esc(m.cooldown_seconds||0)+' s · Datei: '+esc(m.name)+'</div></div>'}).join("");
list.querySelectorAll("[data-act]").forEach(function(el){var m=state.models[Number(el.dataset.i)];if(el.dataset.act==="edit")el.onclick=function(){openEditor(m)};if(el.dataset.act==="del")el.onclick=function(){remove(m)};if(el.dataset.act==="enabled")el.onchange=function(){quickSave(m,{enabled:el.checked})};if(el.dataset.act==="moderation")el.onchange=function(){quickSave(m,{fallback_on_moderation:el.checked})}})}
function payloadOf(m){return {name:m.name,alias:m.alias,type_of_model:m.type_of_model,targets:m.targets,fallback_on_status:m.fallback_on_status,no_fallback_on_status:m.no_fallback_on_status,cooldown_seconds:m.cooldown_seconds,enabled:m.enabled,fallback_on_moderation:m.fallback_on_moderation,moderation_patterns:m.moderation_patterns}}
async function quickSave(m,change){clearMessages();try{var data=await api("PUT",API,Object.assign(payloadOf(m),change));message("good",m.alias+" gespeichert.",data.warnings);await load()}catch(e){message("bad",e.problems?"Ungültiges virtuelles Modell:":e.message,e.problems);render()}}
async function remove(m){if(!confirm("Virtuelles Modell „"+m.alias+"“ löschen?"))return;clearMessages();try{await api("DELETE",API+"?name="+encodeURIComponent(m.name));message("good",m.alias+" gelöscht.");await load()}catch(e){message("bad",e.message)}}
function targetRow(t){var row=document.createElement("div");row.className="t";row.innerHTML='<span class="pos"></span><input type="text" class="tm" list="model-ids" placeholder="Modell-ID" autocomplete="off"><input type="number" class="tt" min="0" step="1" placeholder="Timeout s"><span class="acts row"><button class="btn small" type="button" data-m="up" title="nach oben">&uarr;</button><button class="btn small" type="button" data-m="down" title="nach unten">&darr;</button><button class="btn small danger" type="button" data-m="rm" title="entfernen">&times;</button></span>';
row.querySelector(".tm").value=t.model||"";row.querySelector(".tt").value=t.timeout||"";row.querySelectorAll("[data-m]").forEach(function(b){b.onclick=function(){var box=$("f-targets");if(b.dataset.m==="up"&&row.previousElementSibling)box.insertBefore(row,row.previousElementSibling);if(b.dataset.m==="down"&&row.nextElementSibling)box.insertBefore(row.nextElementSibling,row);if(b.dataset.m==="rm")row.remove();numberTargets()}});return row}
function numberTargets(){var rows=$("f-targets").querySelectorAll(".t");rows.forEach(function(r,i){r.querySelector(".pos").textContent=(i+1)+".";r.querySelector('[data-m="up"]').disabled=i===0;r.querySelector('[data-m="down"]').disabled=i===rows.length-1})}
function openEditor(m){clearMessages();state.editing=m||null;var d=state.defaults||{};m=m||{alias:"",type_of_model:"openai-image",targets:[{model:""},{model:""}],fallback_on_status:d.fallback_on_status||[408,429,500,502,503,504],no_fallback_on_status:[],cooldown_seconds:0,enabled:true,fallback_on_moderation:false,moderation_patterns:d.moderation_patterns||[]};
$("editor-title").textContent=state.editing?"Bearbeiten: "+m.alias:"Neues virtuelles Modell";$("f-alias").value=m.alias;$("f-type").value=m.type_of_model||"openai-image";$("f-cooldown").value=m.cooldown_seconds||0;$("f-fallback").value=(m.fallback_on_status||[]).join(", ");$("f-nofallback").value=(m.no_fallback_on_status||[]).join(", ");$("f-enabled").checked=!!m.enabled;$("f-moderation").checked=!!m.fallback_on_moderation;$("f-patterns").value=(m.moderation_patterns||[]).join("\n");
var box=$("f-targets");box.innerHTML="";(m.targets||[]).forEach(function(t){box.appendChild(targetRow(t))});numberTargets();$("editor").classList.remove("hidden");$("f-alias").focus()}
function closeEditor(){state.editing=null;$("editor").classList.add("hidden")}
async function saveEditor(){clearMessages();var targets=[];$("f-targets").querySelectorAll(".t").forEach(function(r){var model=r.querySelector(".tm").value.trim(),timeout=Number(r.querySelector(".tt").value||0);if(model)targets.push(timeout>0?{model:model,timeout:timeout}:{model:model})});
var body={alias:$("f-alias").value.trim(),type_of_model:$("f-type").value,targets:targets,fallback_on_status:statuses($("f-fallback").value),no_fallback_on_status:statuses($("f-nofallback").value),cooldown_seconds:Number($("f-cooldown").value||0),enabled:$("f-enabled").checked,fallback_on_moderation:$("f-moderation").checked,moderation_patterns:$("f-patterns").value.split("\n").map(function(s){return s.trim()}).filter(Boolean)};
if(state.editing)body.name=state.editing.name;try{var data=await api(state.editing?"PUT":"POST",API,body);message("good",body.alias+" gespeichert.",data.warnings);closeEditor();await load()}catch(e){message("bad",e.problems?"Ungültiges virtuelles Modell:":e.message,e.problems)}}
async function load(){try{var data=await api("GET",API);state.models=data.models||[];state.defaults=data.defaults||{};$("model-ids").innerHTML=(data.available_models||[]).map(function(id){return '<option value="'+esc(id)+'">'}).join("");(data.warnings||[]).forEach(function(w){message("warn",w)});render()}catch(e){$("list").innerHTML='<p class="muted">'+esc(e.message)+'</p>'}}
$("new").onclick=function(){openEditor(null)};$("cancel").onclick=closeEditor;$("save").onclick=saveEditor;$("add-target").onclick=function(){$("f-targets").appendChild(targetRow({}));numberTargets()};$("reload").onclick=function(){clearMessages();load()};
onKey=load;load();
})();
</script></body></html>`
