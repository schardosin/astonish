import os
import requests
import astonish.globals as globals
from astonish.providers.ai_provider_interface import AIProvider
from langchain_ollama import ChatOllama
from typing import List
from rich.prompt import Prompt, IntPrompt
from rich.panel import Panel
from rich.table import Table
from astonish.core.utils import console

class OllamaProvider(AIProvider):
    def __init__(self):
        self.base_url = None

    def setup(self):
        console.print("[bold cyan]Setting up Ollama...[/bold cyan]")

        defaults = {
            'base_url': ('http://localhost:11434', 'http://localhost:11434')
        }

        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)

        if 'OLLAMA' not in globals.config:
            globals.config['OLLAMA'] = {}

        # Input configuration with rich prompts
        for key, (default, example) in defaults.items():
            current_value = globals.config['OLLAMA'].get(key, '')

            prompt_panel = Panel.fit(
                f"[bold magenta]{key.upper()}[/bold magenta]\n"
                f"[dim]Current:[/dim] [green]{current_value or 'None'}[/green]\n"
                f"[dim]Example:[/dim] [italic]{example}[/italic]",
                title="🔧 Configuration Input",
                border_style="cyan"
            )
            console.print(prompt_panel)

            # Inform user how to retain current value
            new_value = Prompt.ask(
                f"[bold cyan]Enter value for {key}[/bold cyan] [dim](leave blank to keep current)[/dim]"
            ).strip()

            globals.config['OLLAMA'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path) + '/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self.base_url = globals.config['OLLAMA']['base_url']

        # Fetch supported models
        supported_models = self.get_supported_models()

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

        if 'GENERAL' not in globals.config:
            globals.config['GENERAL'] = {}

        globals.config['GENERAL']['default_provider'] = 'ollama'
        globals.config['GENERAL']['default_model'] = default_model

        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        # Display a success summary panel
        summary_table = Table(show_header=False, box=None)
        summary_table.add_row("🔌 Default Provider:", f"[bold green]{'ollama'}[/bold green]")
        summary_table.add_row("🤖 Default Model:", f"[bold blue]{default_model}[/bold blue]")

        console.print(Panel.fit(
            summary_table,
            title="✅ [bold green]Configuration Saved Successfully[/bold green]",
            border_style="green"
        ))

    def get_supported_models(self) -> List[str]:
        try:
            response = requests.get(f"{self.base_url}/api/tags")
            response.raise_for_status()
            models = response.json()['models']
            return [model['name'] for model in models]
        except requests.RequestException as e:
            console.print(f"[red]Error fetching models: {e}[/red]")
            return []

    def get_llm(self, model_name: str, streaming: bool = True, schema=None):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)
        
        base_url = globals.config.get('OLLAMA', 'base_url')

        # Initialize and return the Ollama LLM
        llm = ChatOllama(
            base_url=base_url,
            model=model_name,
            num_ctx=8192,
            streaming=streaming,
            format=schema
        )

        return llm
