package app.docveta.android.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewmodel.compose.viewModel
import app.docveta.android.AppContainer
import app.docveta.android.data.ApiException

val LocalContainer = compositionLocalOf<AppContainer> { error("no container") }

class VmFactory<T : ViewModel>(private val make: () -> T) : ViewModelProvider.Factory {
    @Suppress("UNCHECKED_CAST")
    override fun <V : ViewModel> create(modelClass: Class<V>): V = make() as V
}

/** A ViewModel built from the app container. [key] separates instances of the same class (one per document, say). */
@Composable
inline fun <reified T : ViewModel> container(key: String? = null, noinline make: (AppContainer) -> T): T {
    val c = LocalContainer.current
    return viewModel(key = key, factory = VmFactory { make(c) })
}

fun Throwable.friendly(): String = when (this) {
    is ApiException -> message
    is kotlinx.coroutines.CancellationException -> throw this
    else -> "Something went wrong"
}
