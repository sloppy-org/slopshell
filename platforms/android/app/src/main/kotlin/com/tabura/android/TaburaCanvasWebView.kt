package com.tabura.android

import android.content.Context
import android.graphics.Color
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView

@Composable
fun TaburaCanvasWebView(
    html: String,
    baseUrl: String,
    isEinkDisplay: Boolean = false,
    modifier: Modifier = Modifier,
) {
    val renderedHtml = if (isEinkDisplay) {
        applyEinkDisplayHtml(html)
    } else {
        html
    }
    AndroidView(
        modifier = modifier,
        factory = { context ->
            TaburaCanvasDisplayWebView(context).apply {
                setBackgroundColor(Color.TRANSPARENT)
                settings.javaScriptEnabled = false
                settings.allowFileAccess = false
                settings.allowContentAccess = false
                settings.domStorageEnabled = false
                webViewClient = object : WebViewClient() {
                    override fun onPageFinished(view: WebView, url: String?) {
                        super.onPageFinished(view, url)
                        (view as? TaburaCanvasDisplayWebView)?.onContentRendered()
                    }
                }
            }
        },
        update = { view ->
            view.setEinkDisplay(isEinkDisplay)
            view.loadDataWithBaseURL(baseUrl, renderedHtml, "text/html", "utf-8", null)
        },
    )
}

private class TaburaCanvasDisplayWebView(
    context: Context,
) : WebView(context) {
    private var isEinkDisplay = false
    private var pendingRefresh: Runnable? = null

    fun setEinkDisplay(enabled: Boolean) {
        isEinkDisplay = enabled
        if (enabled) {
            TaburaBooxEinkController.configureContentView(this)
            TaburaBooxEinkController.setWebViewContrastOptimize(this, true)
            return
        }
        pendingRefresh?.let(::removeCallbacks)
        pendingRefresh = null
        TaburaBooxEinkController.setWebViewContrastOptimize(this, false)
    }

    fun onContentRendered() {
        if (!isEinkDisplay) {
            return
        }
        TaburaBooxEinkController.configureContentView(this)
        scheduleRefresh()
    }

    override fun onDetachedFromWindow() {
        pendingRefresh?.let(::removeCallbacks)
        pendingRefresh = null
        super.onDetachedFromWindow()
    }

    private fun scheduleRefresh() {
        pendingRefresh?.let(::removeCallbacks)
        val refresh = Runnable {
            TaburaBooxEinkController.refreshContentView(this)
        }
        pendingRefresh = refresh
        postDelayed(refresh, 160L)
    }
}

private fun applyEinkDisplayHtml(html: String): String {
    val bodyMatch = BODY_TAG.find(html)
    val withBodyClass = when {
        bodyMatch != null -> {
            val attributes = bodyMatch.groupValues[1]
            val replacement = if (BODY_CLASS_TAG.containsMatchIn(attributes)) {
                val classMatch = BODY_CLASS_TAG.find(attributes)
                val classes = classMatch?.groupValues?.getOrNull(1)
                    ?.split(Regex("\\s+"))
                    ?.filter { it.isNotBlank() }
                    ?.toMutableList()
                    ?: mutableListOf()
                if (!classes.contains("eink-display")) {
                    classes += "eink-display"
                }
                val updatedAttributes = classMatch?.range?.let { range ->
                    attributes.replaceRange(range, "class=\"${classes.joinToString(" ")}\"")
                } ?: attributes
                "<body$updatedAttributes>"
            } else {
                "<body$attributes class=\"eink-display\">"
            }
            html.replaceRange(bodyMatch.range, replacement)
        }
        else -> "<html><body class=\"eink-display\">$html</body></html>"
    }
    val headMatch = HEAD_END_TAG.find(withBodyClass)
    return if (headMatch != null) {
        withBodyClass.replaceRange(headMatch.range, "$EINK_STYLE</head>")
    } else {
        val nextBodyMatch = BODY_TAG.find(withBodyClass)
        if (nextBodyMatch != null) {
            withBodyClass.replaceRange(nextBodyMatch.range, "<head>$EINK_STYLE</head>${nextBodyMatch.value}")
        } else {
            "<head>$EINK_STYLE</head>$withBodyClass"
        }
    }
}

private val BODY_TAG = Regex("<body([^>]*)>", RegexOption.IGNORE_CASE)
private val BODY_CLASS_TAG = Regex("class\\s*=\\s*\"([^\"]*)\"", RegexOption.IGNORE_CASE)
private val HEAD_END_TAG = Regex("</head>", RegexOption.IGNORE_CASE)
private val EINK_STYLE = """
<style>
html, body {
  background: #fff !important;
  color: #000 !important;
}
body.eink-display,
body.eink-display * {
  transition: none !important;
  animation: none !important;
  background-image: none !important;
  box-shadow: none !important;
  text-shadow: none !important;
  filter: none !important;
  scroll-behavior: auto !important;
}
body.eink-display a,
body.eink-display pre,
body.eink-display code,
body.eink-display table,
body.eink-display th,
body.eink-display td,
body.eink-display blockquote,
body.eink-display hr {
  color: #000 !important;
  border-color: #000 !important;
}
body.eink-display [style*="gradient"],
body.eink-display [style*="opacity"],
body.eink-display [style*="shadow"] {
  background: #fff !important;
  opacity: 1 !important;
}
</style>
""".trimIndent()
