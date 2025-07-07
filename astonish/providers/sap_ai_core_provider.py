import os
import astonish.globals as globals
from typing import List, Callable, Dict

# Import all the specific initializers we might need
from gen_ai_hub.proxy.langchain.init_models import init_llm
from gen_ai_hub.proxy.langchain.openai import init_chat_model as openai_init_chat_model
from gen_ai_hub.proxy.langchain.google_vertexai import init_chat_model as google_vertexai_init_chat_model
from gen_ai_hub.proxy.langchain.amazon import init_chat_model as amazon_init_invoke_model, init_chat_converse_model as amazon_init_converse_model
from gen_ai_hub.proxy.core.proxy_clients import get_proxy_client
from astonish.providers.ai_provider_interface import AIProvider
from astonish.providers.exceptions import SapAICoreRateLimitError
from rich.prompt import Prompt, IntPrompt
from rich.panel import Panel
from rich.table import Table
from astonish.core.utils import console

class SAPAICoreProvider(AIProvider):
    # A mapping for model names to specific, required model_id strings.
    # This allows overriding the default SDK behavior for new or unsupported models.
    MODEL_ID_MAP: Dict[str, str] = {
        'anthropic--claude-4-sonnet': 'anthropic.claude-sonnet-4-20250514-v1:0',
        'o1': 'o1',
        'o4-mini': 'o4-mini',
    }

    def __init__(self):
        self.proxy_client = None

    def setup(self):
        console.print("[bold cyan]Setting up SAP AI Core...[/bold cyan]")
        
        defaults = {
            'client_id': ('', 'your-client-id'),
            'client_secret': ('', 'your-client-secret'),
            'auth_url': ('', 'https://<tenant-id>.authentication.sap.hana.ondemand.com'),
            'base_url': ('', 'https://api.ai.internalprod.eu-central-1.aws.ml.hana.ondemand.com'),
            'resource_group': ('default', 'default')
        }

        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)
        
        if 'SAP_AI_CORE' not in globals.config:
            globals.config['SAP_AI_CORE'] = {}

        # Input new values with rich library
        for key, (default, example) in defaults.items():
            current_value = globals.config['SAP_AI_CORE'].get(key, '')
            
            prompt_panel = Panel.fit(
                f"[bold magenta]{key.upper()}[/bold magenta]\n"
                f"[dim]Current:[/dim] [green]{current_value or 'None'}[/green]\n"
                f"[dim]Example:[/dim] [italic]{example}[/italic]",
                title="🔧 Configuration Input",
                border_style="cyan"
            )
            console.print(prompt_panel)

            new_value = Prompt.ask(
                f"[bold cyan]Enter value for {key}[/bold cyan] [dim](leave blank to keep current)[/dim]"
            ).strip()

            globals.config['SAP_AI_CORE'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path) + '/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self._initialize_proxy_client()

        # Get supported models
        supported_models = self.get_supported_models()

        # Display supported models in a table
        console.print("\n[bold yellow]Supported models:[/bold yellow]")
        for i, model in enumerate(supported_models, 1):
            console.print(f"{i}. {model}")

        while True:
            try:
                selection = IntPrompt.ask(
                    "\n[bold yellow]🔢 Select the number of the model you want to use as default[/bold yellow]"
                )
                if 1 <= selection <= len(supported_models):
                    default_model = supported_models[selection - 1]
                    break
                else:
                    console.print("[red]❌ Invalid selection. Please choose a number from the list.[/red]")
            except ValueError:
                console.print("[red]⚠️ Invalid input. Please enter a number.[/red]")

        # Ensure GENERAL section exists
        if 'GENERAL' not in globals.config:
            globals.config['GENERAL'] = {}

        globals.config['GENERAL']['default_provider'] = 'sap_ai_core'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        # Display success summary panel
        summary_table = Table(show_header=False, box=None)
        summary_table.add_row("🔌 Default Provider:", f"[bold green]{'sap_ai_core'}[/bold green]")
        summary_table.add_row("🤖 Default Model:", f"[bold blue]{default_model}[/bold blue]")

        console.print(Panel.fit(
            summary_table,
            title="✅ [bold green]Configuration Saved Successfully[/bold green]",
            border_style="green"
        ))


    def _initialize_proxy_client(self):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)

        # Set environment variables from the configuration
        os.environ["AICORE_AUTH_URL"] = globals.config.get('SAP_AI_CORE', 'auth_url')
        os.environ["AICORE_CLIENT_ID"] = globals.config.get('SAP_AI_CORE', 'client_id')
        os.environ["AICORE_CLIENT_SECRET"] = globals.config.get('SAP_AI_CORE', 'client_secret')
        os.environ["AICORE_RESOURCE_GROUP"] = globals.config.get('SAP_AI_CORE', 'resource_group')
        base_url = globals.config.get('SAP_AI_CORE', 'base_url')
        if not base_url.endswith('/v2'):
            base_url += '/v2'
        os.environ["AICORE_BASE_URL"] = globals.config.get('SAP_AI_CORE', 'base_url')

        # Initialize the proxy client
        self.proxy_client = get_proxy_client("gen-ai-hub")
        self.proxy_client.refresh_instance_cache()

    def get_supported_models(self) -> List[str]:
        if not self.proxy_client:
            self._initialize_proxy_client()

        model_names = [deployment.model_name for deployment in self.proxy_client.deployments]
        
        return sorted(list(set(model_names)))

    def _get_init_func_for_model(self, model_name: str) -> Callable:
        """
        Resolves the correct initialization function based on the model name.
        This works around a bug in the SDK's default model resolution.
        """
        # A list of models known to use the newer Bedrock Converse API
        converse_api_models = ['claude-3.7', 'claude-4']

        # --- Anthropic models are routed via Amazon Bedrock ---
        # Check for Anthropic prefix or if the model is in our manual list
        if model_name.startswith('anthropic--') or model_name in converse_api_models:
            # Check if the model name contains any known Converse API model identifiers
            if any(m in model_name for m in converse_api_models):
                return amazon_init_converse_model
            # Older models use the older Invoke API
            else:
                return amazon_init_invoke_model
        
        elif model_name.startswith('gemini-'):
            return google_vertexai_init_chat_model
        elif model_name.startswith('amazon--'):
            # This handles non-Anthropic Bedrock models like Titan
            return amazon_init_invoke_model
        else:
            return openai_init_chat_model

    def get_llm(self, model_name: str, streaming: bool = True):
        """
        Dynamically initializes any supported LLM by resolving the correct
        init function and passing a specific model_id when mapped.
        """
        if not self.proxy_client:
            self._initialize_proxy_client()

        if model_name in ["o1", "o3-mini", "o3", "o4-mini", "o4"]:
            temperature = 1
        else:
            temperature = 0.7

        initializer_function = self._get_init_func_for_model(model_name)

        # Prepare keyword arguments for the init_llm call
        init_kwargs = {
            "proxy_client": self.proxy_client,
            "streaming": streaming,
            "max_tokens": 32768,
            "temperature": temperature
        }

        # Only set init_func if the model is in MODEL_ID_MAP
        if model_name in self.MODEL_ID_MAP:
            init_kwargs["init_func"] = initializer_function

        # If the model_name has a specific model_id mapped, add it to the kwargs.
        # This allows for using models not yet fully supported by the SDK.
        if model_name in self.MODEL_ID_MAP and model_name.startswith('anthropic--'):
            init_kwargs["model_id"] = self.MODEL_ID_MAP[model_name]

        try:
            # Call init_llm by unpacking the prepared arguments
            llm = init_llm(model_name, **init_kwargs)

        except Exception as e:
            print(f"Error initializing LLM '{model_name}': {str(e)}")
            raise SapAICoreRateLimitError(f"Failed to initialize LLM '{model_name}': {str(e)}") from e

        # Monkey-patch ainvoke to add custom rate limit error handling
        orig_ainvoke = llm.ainvoke

        async def ainvoke_with_rate_limit_check(*args, **kwargs):
            try:
                result = await orig_ainvoke(*args, **kwargs)
                if hasattr(result, "content") and isinstance(result.content, str):
                    if "Your request has been rate limited by AI Core" in result.content:
                        raise SapAICoreRateLimitError("Your request has been rate limited by AI Core. Please try again later.")
                return result
            except Exception as e:
                if "Your request has been rate limited by AI Core" in str(e):
                    raise SapAICoreRateLimitError("Your request has been rate limited by AI Core. Please try again later.") from e
                raise

        llm.ainvoke = ainvoke_with_rate_limit_check
        return llm